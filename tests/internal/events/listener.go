// Package events verifies side effects published to Kafka (Redpanda).
//
// A Listener is created BEFORE the action under test and reads the topic
// from a few seconds in the past, so an event produced right after the
// action is never missed. Events are filtered by record key (the account
// id), and every test owns its account, so parallel tests never see each
// other's events. Waiting is bounded by a deadline: no sleeps.
package events

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/7jarvis/tradebench/tests/internal/config"
	"github.com/7jarvis/tradebench/tests/internal/models"
)

type Listener struct {
	t       *testing.T
	client  *kgo.Client
	timeout time.Duration
	seen    map[string][]models.TradeUpdate // by account id
}

// Listen subscribes to the trade-updates topic. Skips the test when no
// Kafka brokers are configured (e.g. a run without the event pipeline).
func Listen(t *testing.T) *Listener {
	t.Helper()
	cfg := config.Get()
	if len(cfg.KafkaBrokers) == 0 && cfg.RequireEvents {
		t.Fatal("REQUIRE_EVENTS is set but KAFKA_BROKERS is empty")
	}
	if len(cfg.KafkaBrokers) == 0 {
		t.Skip("KAFKA_BROKERS is not set; event assertions need the full stack (make up)")
	}
	since := time.Now().Add(-5 * time.Second)
	client, err := kgo.NewClient(
		kgo.SeedBrokers(cfg.KafkaBrokers...),
		kgo.ConsumeTopics(cfg.KafkaTopic),
		kgo.ConsumeResetOffset(kgo.NewOffset().AfterMilli(since.UnixMilli())),
		kgo.AllowAutoTopicCreation(),
	)
	if err != nil {
		t.Fatalf("events: kafka client: %v", err)
	}
	t.Cleanup(client.Close)
	return &Listener{t: t, client: client, timeout: cfg.EventTimeout, seen: map[string][]models.TradeUpdate{}}
}

// Expect waits until the account has emitted events matching the given
// sequence of event types, in order, and returns them. Extra events of
// other orders are ignored only if they do not interleave — the exact
// stream for the account is printed on failure.
func (l *Listener) Expect(accountID string, wantEvents ...string) []models.TradeUpdate {
	l.t.Helper()
	deadline := time.Now().Add(l.timeout)
	for {
		got := l.seen[accountID]
		if len(got) >= len(wantEvents) {
			if gotTypes := types(got); strings.Join(gotTypes, ",") != strings.Join(wantEvents, ",") {
				l.t.Fatalf("events for account %s: got %v, want %v", accountID, gotTypes, wantEvents)
			}
			return got
		}
		if time.Now().After(deadline) {
			l.t.Fatalf("events for account %s: timed out after %s; got %v, want %v",
				accountID, l.timeout, types(got), wantEvents)
		}
		l.poll(time.Until(deadline))
	}
}

func (l *Listener) poll(max time.Duration) {
	l.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), min(max, time.Second))
	defer cancel()
	fetches := l.client.PollFetches(ctx)
	for _, fe := range fetches.Errors() {
		if !errors.Is(fe.Err, context.DeadlineExceeded) && !errors.Is(fe.Err, context.Canceled) {
			l.t.Logf("events: fetch error on %s/%d: %v", fe.Topic, fe.Partition, fe.Err)
		}
	}
	fetches.EachRecord(func(r *kgo.Record) {
		u, err := models.Decode[models.TradeUpdate](r.Value)
		if err != nil {
			// A malformed event is a contract bug in the producer: fail loudly.
			l.t.Errorf("events: %v", err)
			return
		}
		key := string(r.Key)
		if key != u.Order.AccountID {
			l.t.Errorf("events: record key %q != order.account_id %q (breaks per-account ordering)", key, u.Order.AccountID)
		}
		l.seen[key] = append(l.seen[key], u)
		l.t.Logf("⇐ kafka %s offset=%d key=%s event=%s order=%s", r.Topic, r.Offset, key, u.Event, u.Order.ID)
	})
}

func types(us []models.TradeUpdate) []string {
	out := make([]string, len(us))
	for i, u := range us {
		out[i] = u.Event
	}
	return out
}

// String helps debugging in failure messages.
func (l *Listener) String() string { return fmt.Sprintf("listener(%d accounts seen)", len(l.seen)) }
