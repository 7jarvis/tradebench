// Package api_test holds black-box API tests. They talk only to the service
// layer (tests/internal/service) and never import service code: the system
// under test is reached over the network exactly as a client would.
package api_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/7jarvis/tradebench/tests/internal/adapter"
	"github.com/7jarvis/tradebench/tests/internal/config"
	"github.com/7jarvis/tradebench/tests/internal/httpclient"
	"github.com/7jarvis/tradebench/tests/internal/service"
)

func TestMain(m *testing.M) {
	if err := waitReady(60 * time.Second); err != nil {
		fmt.Fprintf(os.Stderr, "service at %s is not ready: %v\nStart the stack with `make up`.\n", config.Get().BaseURL, err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// waitReady polls /readyz with a deadline instead of sleeping a fixed time.
func waitReady(timeout time.Duration) error {
	cfg := config.Get()
	b := service.NewBroker(adapter.NewBroker(httpclient.New(cfg.BaseURL, httpclient.WithTimeout(2*time.Second)), ""))
	deadline := time.Now().Add(timeout)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := b.Ready(ctx)
		cancel()
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return err
		}
		time.Sleep(500 * time.Millisecond) // polling interval, not a fixed wait for state
	}
}
