// Command broker runs the sandbox brokerage API used as the system under test.
//
//	broker              start the server
//	broker healthcheck  exit 0 if the local server is ready (for container healthchecks)
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	_ "net/http/pprof" // registered on http.DefaultServeMux, served only on PPROF_ADDR
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/7jarvis/tradebench/service/internal/api"
	"github.com/7jarvis/tradebench/service/internal/broker"
	"github.com/7jarvis/tradebench/service/internal/config"
	"github.com/7jarvis/tradebench/service/internal/db"
	"github.com/7jarvis/tradebench/service/internal/events"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := newLogger(cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool); err != nil {
		return err
	}

	if len(cfg.KafkaBrokers) > 0 {
		relay, err := events.NewRelay(pool, cfg.KafkaBrokers, log)
		if err != nil {
			return fmt.Errorf("kafka client: %w", err)
		}
		go relay.Run(ctx)
		log.Info("outbox relay started", "brokers", cfg.KafkaBrokers, "topic", cfg.KafkaTopic)
	} else {
		log.Warn("KAFKA_BROKERS not set: events stay in the outbox and are not published")
	}

	if cfg.PprofAddr != "" {
		go func() {
			log.Info("pprof listening", "addr", cfg.PprofAddr)
			_ = http.ListenAndServe(cfg.PprofAddr, http.DefaultServeMux) //nolint:gosec // admin port, not exposed publicly
		}()
	}

	svc := broker.NewService(pool, cfg.KafkaTopic)
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           api.NewHandler(svc, pool, cfg.APIToken, log),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("http listening", "addr", cfg.HTTPAddr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
	return nil
}

func newLogger(level string) *slog.Logger {
	var l slog.Level
	if err := l.UnmarshalText([]byte(level)); err != nil {
		l = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: l}))
}

func healthcheck() int {
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return 1
	}
	client := http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + port + "/readyz")
	if err != nil {
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
