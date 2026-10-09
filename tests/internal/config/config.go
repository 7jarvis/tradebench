// Package config reads test-run settings from the environment.
//
// Defaults target the local docker compose stack. CI overrides them via env;
// no real credentials are ever committed.
package config

import (
	"os"
	"strings"
	"sync"
	"time"
)

type Config struct {
	BaseURL      string        // BASE_URL
	APIToken     string        // API_TOKEN
	KafkaBrokers []string      // KAFKA_BROKERS; empty => event tests are skipped
	KafkaTopic   string        // KAFKA_TOPIC
	HTTPTimeout  time.Duration // HTTP_TIMEOUT, e.g. "10s"
	EventTimeout time.Duration // EVENT_TIMEOUT, how long to wait for a Kafka event
}

var (
	once sync.Once
	cfg  Config
)

// Get returns the process-wide config, read once.
func Get() Config {
	once.Do(func() {
		cfg = Config{
			BaseURL:      strings.TrimRight(env("BASE_URL", "http://localhost:8080"), "/"),
			APIToken:     env("API_TOKEN", "local-dev-token"),
			KafkaTopic:   env("KAFKA_TOPIC", "trade-updates"),
			HTTPTimeout:  duration("HTTP_TIMEOUT", 10*time.Second),
			EventTimeout: duration("EVENT_TIMEOUT", 10*time.Second),
		}
		for _, b := range strings.Split(os.Getenv("KAFKA_BROKERS"), ",") {
			if b = strings.TrimSpace(b); b != "" {
				cfg.KafkaBrokers = append(cfg.KafkaBrokers, b)
			}
		}
	})
	return cfg
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func duration(key string, def time.Duration) time.Duration {
	if d, err := time.ParseDuration(os.Getenv(key)); err == nil {
		return d
	}
	return def
}
