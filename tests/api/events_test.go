package api_test

import (
	"testing"

	"github.com/7jarvis/tradebench/tests/internal/builders"
	"github.com/7jarvis/tradebench/tests/internal/check"
	"github.com/7jarvis/tradebench/tests/internal/events"
	"github.com/7jarvis/tradebench/tests/internal/fixtures"
)

// Side effects: every order state change must reach the trade-updates topic,
// in order, keyed by account. Downstream consumers (statements, risk,
// notifications) depend on this stream, so "the API said filled" is not
// enough.

func TestMarketBuy_PublishesNewThenFill(t *testing.T) {
	t.Parallel()
	listener := events.Listen(t) // before the action
	broker := fixtures.Broker(t)
	acc := fixtures.FundedAccount(t, broker, "1000.00")
	sym := fixtures.Symbol(t, broker, "25.00")

	order, err := broker.PlaceOrder(t.Context(), acc.ID, builders.MarketBuy(sym, 4))
	check.NoError(t, err, "place market buy")

	got := listener.Expect(acc.ID, "new", "fill")
	check.Equal(t, "new: order id", got[0].Order.ID, order.ID)
	check.Equal(t, "new: order status", got[0].Order.Status, "new")
	check.Equal(t, "fill: order id", got[1].Order.ID, order.ID)
	check.Equal(t, "fill: price", check.Value(t, "fill price", got[1].Price), "25.00")
	check.Equal(t, "fill: qty", check.Value(t, "fill qty", got[1].Qty), "4")
	check.DeepEqual(t, "fill: order payload equals API order", got[1].Order, order)
}

func TestCancel_PublishesCanceled(t *testing.T) {
	t.Parallel()
	listener := events.Listen(t)
	broker := fixtures.Broker(t)
	acc := fixtures.FundedAccount(t, broker, "1000.00")
	sym := fixtures.Symbol(t, broker, "25.00")
	order, err := broker.PlaceOrder(t.Context(), acc.ID, builders.LimitBuy(sym, 1, "20.00"))
	check.NoError(t, err, "place resting order")

	check.NoError(t, broker.CancelOrder(t.Context(), acc.ID, order.ID), "cancel")

	got := listener.Expect(acc.ID, "new", "canceled")
	check.Equal(t, "canceled: order id", got[1].Order.ID, order.ID)
	check.True(t, got[1].Order.CanceledAt != nil, "canceled event must carry canceled_at")
}

func TestLimitFillAfterPriceMove_PublishesFillAtNewPrice(t *testing.T) {
	t.Parallel()
	listener := events.Listen(t)
	broker := fixtures.Broker(t)
	acc := fixtures.FundedAccount(t, broker, "1000.00")
	sym := fixtures.Symbol(t, broker, "25.00")
	_, err := broker.PlaceOrder(t.Context(), acc.ID, builders.LimitBuy(sym, 2, "20.00"))
	check.NoError(t, err, "place resting order")

	_, err = broker.SetPrice(t.Context(), sym, "19.99")
	check.NoError(t, err, "cross the limit")

	got := listener.Expect(acc.ID, "new", "fill")
	check.Equal(t, "fill price", check.Value(t, "fill price", got[1].Price), "19.99")
}

func TestRejectedOrder_PublishesNothing(t *testing.T) {
	t.Parallel()
	listener := events.Listen(t)
	broker := fixtures.Broker(t)
	acc := fixtures.FundedAccount(t, broker, "1.00")
	sym := fixtures.Symbol(t, broker, "25.00")

	_, err := broker.PlaceOrder(t.Context(), acc.ID, builders.MarketBuy(sym, 1))
	check.True(t, err != nil, "order must be rejected")
	// Marker action proves the stream is flowing for this account; the
	// rejected order must not appear before it.
	_, err = broker.PlaceOrder(t.Context(), acc.ID, builders.LimitBuy(sym, 1, "0.50"))
	check.NoError(t, err, "marker order")

	got := listener.Expect(acc.ID, "new")
	check.Equal(t, "only the marker order is published", got[0].Order.Symbol+"/"+got[0].Order.Type, sym+"/limit")
}
