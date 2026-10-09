package api_test

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/7jarvis/tradebench/tests/internal/builders"
	"github.com/7jarvis/tradebench/tests/internal/check"
	"github.com/7jarvis/tradebench/tests/internal/fixtures"
	"github.com/7jarvis/tradebench/tests/internal/service"
)

// Double-spend guard: many simultaneous buys against one account must never
// spend more than the account has. Sequential tests cannot catch a missing
// row lock; this one does.
func TestConcurrentBuys_NeverOverspendBuyingPower(t *testing.T) {
	t.Parallel()
	const (
		attempts   = 20
		affordable = 5
	)
	broker := fixtures.Broker(t)
	acc := fixtures.FundedAccount(t, broker, "500.00") // exactly 5 x 100.00
	sym := fixtures.Symbol(t, broker, "100.00")

	errs := runConcurrently(t.Context(), attempts, func(ctx context.Context) error {
		_, err := broker.PlaceOrder(ctx, acc.ID, builders.MarketBuy(sym, 1))
		return err
	})

	filled, rejected := 0, 0
	for _, err := range errs {
		switch e, ok := service.AsAPIError(err); {
		case err == nil:
			filled++
		case ok && e.Status == http.StatusForbidden && e.Code == "insufficient_buying_power":
			rejected++
		default:
			t.Errorf("unexpected outcome: %v", err)
		}
	}
	check.Equal(t, "filled", filled, affordable)
	check.Equal(t, "rejected", rejected, attempts-affordable)

	after, err := broker.GetAccount(t.Context(), acc.ID)
	check.NoError(t, err, "get account")
	check.Equal(t, "cash", after.Cash, "0.00")
	pos, err := broker.Position(t.Context(), acc.ID, sym)
	check.NoError(t, err, "get position")
	check.Equal(t, "position qty", pos.Qty, "5")
}

// Idempotency key under a race: the same client_order_id sent N times at
// once creates exactly one order.
func TestConcurrentDuplicateClientOrderID_ExactlyOneOrder(t *testing.T) {
	t.Parallel()
	const attempts = 10
	broker := fixtures.Broker(t)
	acc := fixtures.FundedAccount(t, broker, "1000.00")
	sym := fixtures.Symbol(t, broker, "1.00")
	req := builders.MarketBuy(sym, 1)
	req.ClientOrderID = "retry-storm-1"

	errs := runConcurrently(t.Context(), attempts, func(ctx context.Context) error {
		_, err := broker.PlaceOrder(ctx, acc.ID, req)
		return err
	})

	created := 0
	for _, err := range errs {
		if err == nil {
			created++
			continue
		}
		if e, ok := service.AsAPIError(err); !ok || e.Code != "duplicate_client_order_id" {
			t.Errorf("unexpected outcome: %v", err)
		}
	}
	check.Equal(t, "orders created", created, 1)
	orders, err := broker.ListOrders(t.Context(), acc.ID, "all")
	check.NoError(t, err, "list orders")
	check.Equal(t, "orders persisted", len(orders), 1)
}

// runConcurrently starts n calls at the same moment and returns their errors.
func runConcurrently(ctx context.Context, n int, call func(context.Context) error) []error {
	errs := make([]error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs[i] = call(ctx)
		}()
	}
	close(start)
	wg.Wait()
	return errs
}
