// Package fixtures wires the layers for a test and creates isolated test
// data with automatic cleanup.
//
// Isolation rule: every test gets its own account and, when it moves prices,
// its own sandbox symbol. Tests therefore run with t.Parallel() and never
// depend on each other or on execution order.
package fixtures

import (
	"context"
	"crypto/rand"
	"strings"
	"testing"

	"github.com/7jarvis/tradebench/tests/internal/adapter"
	"github.com/7jarvis/tradebench/tests/internal/builders"
	"github.com/7jarvis/tradebench/tests/internal/config"
	"github.com/7jarvis/tradebench/tests/internal/httpclient"
	"github.com/7jarvis/tradebench/tests/internal/models"
	"github.com/7jarvis/tradebench/tests/internal/service"
)

// Broker returns the service layer wired with this test's logger, so HTTP
// traffic appears in the test output on failure.
func Broker(t *testing.T) *service.Broker {
	t.Helper()
	return service.NewBroker(newAdapter(t, config.Get().APIToken))
}

// BrokerWithToken authenticates with token instead ("" sends no header).
func BrokerWithToken(t *testing.T, token string) *service.Broker {
	t.Helper()
	return service.NewBroker(newAdapter(t, token))
}

func newAdapter(t *testing.T, token string) *adapter.Broker {
	cfg := config.Get()
	c := httpclient.New(cfg.BaseURL, httpclient.WithLogger(t), httpclient.WithTimeout(cfg.HTTPTimeout))
	return adapter.NewBroker(c, token)
}

// Account opens an empty account named after the test and closes it at the
// end (closing also cancels any open orders it left behind).
func Account(t *testing.T, b *service.Broker) models.Account {
	t.Helper()
	acc, err := b.OpenAccount(t.Context(), ownerName(t))
	if err != nil {
		t.Fatalf("fixture: open account: %v", err)
	}
	t.Cleanup(func() {
		// t.Context() is already canceled when cleanups run.
		if err := b.CloseAccount(context.Background(), acc.ID); err != nil {
			t.Logf("fixture cleanup: close account %s: %v", acc.ID, err)
		}
	})
	return acc
}

// FundedAccount opens an account and deposits amount.
func FundedAccount(t *testing.T, b *service.Broker, amount string) models.Account {
	t.Helper()
	acc := Account(t, b)
	acc, err := b.Deposit(t.Context(), acc.ID, amount)
	if err != nil {
		t.Fatalf("fixture: deposit %s: %v", amount, err)
	}
	return acc
}

// Symbol creates a sandbox symbol owned by this test, priced at price.
// Moving its price cannot affect any other test.
func Symbol(t *testing.T, b *service.Broker, price string) string {
	t.Helper()
	sym := "Q" + randomLetters(9)
	if _, err := b.SetPrice(t.Context(), sym, price); err != nil {
		t.Fatalf("fixture: create symbol %s: %v", sym, err)
	}
	return sym
}

// Position buys qty of a fresh symbol at price so the account holds it.
// Returns the symbol. The account must have enough buying power.
func Position(t *testing.T, b *service.Broker, accountID string, qty int, price string) string {
	t.Helper()
	sym := Symbol(t, b, price)
	if _, err := b.PlaceOrder(t.Context(), accountID, builders.MarketBuy(sym, qty)); err != nil {
		t.Fatalf("fixture: buy %d %s: %v", qty, sym, err)
	}
	return sym
}

func ownerName(t *testing.T) string {
	name := "qa " + t.Name()
	if len(name) > 100 {
		name = name[:100]
	}
	return name
}

func randomLetters(n int) string {
	const letters = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	b := make([]byte, n)
	_, _ = rand.Read(b)
	var sb strings.Builder
	for _, c := range b {
		sb.WriteByte(letters[int(c)%len(letters)])
	}
	return sb.String()
}
