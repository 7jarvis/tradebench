// Package config reads service settings from environment variables.
package config

import (
	"errors"
	"os"
	"strings"
)

type Config struct {
	HTTPAddr     string   // HTTP_ADDR, default ":8080"
	DatabaseURL  string   // DATABASE_URL, required
	APIToken     string   // API_TOKEN, required; clients send "Authorization: Bearer <token>"
	KafkaBrokers []string // KAFKA_BROKERS, comma separated; empty disables event publishing
	KafkaTopic   string   // KAFKA_TOPIC, default "trade-updates"
	PprofAddr    string   // PPROF_ADDR, optional admin listener for net/http/pprof
	LogLevel     string   // LOG_LEVEL: debug|info|warn|error, default "info"
}

func Load() (Config, error) {
	c := Config{
		HTTPAddr:    env("HTTP_ADDR", ":8080"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		APIToken:    os.Getenv("API_TOKEN"),
		KafkaTopic:  env("KAFKA_TOPIC", "trade-updates"),
		PprofAddr:   os.Getenv("PPROF_ADDR"),
		LogLevel:    env("LOG_LEVEL", "info"),
	}
	if b := strings.TrimSpace(os.Getenv("KAFKA_BROKERS")); b != "" {
		for _, s := range strings.Split(b, ",") {
			if s = strings.TrimSpace(s); s != "" {
				c.KafkaBrokers = append(c.KafkaBrokers, s)
			}
		}
	}

	var errs []error
	if c.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required"))
	}
	if c.APIToken == "" {
		errs = append(errs, errors.New("API_TOKEN is required"))
	}
	return c, errors.Join(errs...)
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
