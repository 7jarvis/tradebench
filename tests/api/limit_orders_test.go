package api_test

import (
	"net/http"
	"testing"

	"github.com/7jarvis/tradebench/tests/internal/builders"
	"github.com/7jarvis/tradebench/tests/internal/check"
	"github.com/7jarvis/tradebench/tests/internal/fixtures"
	"github.com/7jarvis/tradebench/tests/internal/money"
)

func TestLimitBuy_BelowMarket_RestsAndReservesBuyingPower(t *testing.T) {
	t.Parallel()
	broker := fixtures.Broker(t)
	acc := fixtures.FundedAccount(t, broker, "1000.00")
	sym := fixtures.Symbol(t, broker, "100.00")

	order, err := broker.PlaceOrder(t.Context(), acc.ID, builders.LimitBuy(sym, 5, "90.00"))
	check.NoError(t, err, "place limit buy below market")

	check.Equal(t, "status", order.Status, "new")
	check.Equal(t, "filled_qty", order.FilledQty, "0")
	check.Nil(t, "filled_avg_price", order.FilledAvgPrice)

	after, err := broker.GetAccount(t.Context(), acc.ID)
	check.NoError(t, err, "get account")
	check.Equal(t, "cash (nothing spent yet)", after.Cash, "1000.00")
	check.Equal(t, "buying_power (limit*qty reserved)", after.BuyingPower,
		money.Of(t, "1000.00").Sub(money.Of(t, "90.00").Mul(5)).String())
}

func TestLimitBuy_AtOrAboveMarket_FillsImmediatelyAtMarketPrice(t *testing.T) {
	t.Parallel()
	for name, limit := range map[string]string{"at market": "100.00", "above market": "120.00"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			broker := fixtures.Broker(t)
			acc := fixtures.FundedAccount(t, broker, "1000.00")
			sym := fixtures.Symbol(t, broker, "100.00")

			order, err := broker.PlaceOrder(t.Context(), acc.ID, builders.LimitBuy(sym, 2, limit))
			check.NoError(t, err, "place marketable limit buy")

			check.Equal(t, "status", order.Status, "filled")
			// Price improvement: the client pays the market price, not the limit.
			check.Equal(t, "filled_avg_price", check.Value(t, "filled_avg_price", order.FilledAvgPrice), "100.00")
		})
	}
}

func TestLimitBuy_FillsWhenPriceDropsToLimit(t *testing.T) {
	t.Parallel()
	broker := fixtures.Broker(t)
	acc := fixtures.FundedAccount(t, broker, "1000.00")
	sym := fixtures.Symbol(t, broker, "100.00")
	order, err := broker.PlaceOrder(t.Context(), acc.ID, builders.LimitBuy(sym, 5, "90.00"))
	check.NoError(t, err, "place resting limit buy")

	_, err = broker.SetPrice(t.Context(), sym, "90.01")
	check.NoError(t, err, "move price close to limit")
	got, err := broker.GetOrder(t.Context(), acc.ID, order.ID)
	check.NoError(t, err, "get order")
	check.Equal(t, "status one cent above limit", got.Status, "new")

	_, err = broker.SetPrice(t.Context(), sym, "89.50")
	check.NoError(t, err, "move price through limit")

	got, err = broker.GetOrder(t.Context(), acc.ID, order.ID)
	check.NoError(t, err, "get order")
	check.Equal(t, "status", got.Status, "filled")
	check.Equal(t, "filled_avg_price", check.Value(t, "filled_avg_price", got.FilledAvgPrice), "89.50")

	after, err := broker.GetAccount(t.Context(), acc.ID)
	check.NoError(t, err, "get account")
	wantCash := money.Of(t, "1000.00").Sub(money.Of(t, "89.50").Mul(5)).String()
	check.Equal(t, "cash", after.Cash, wantCash)
	check.Equal(t, "buying_power (reservation released)", after.BuyingPower, wantCash)
}

func TestLimitSell_FillsWhenPriceRisesToLimit(t *testing.T) {
	t.Parallel()
	broker := fixtures.Broker(t)
	acc := fixtures.FundedAccount(t, broker, "1000.00")
	sym := fixtures.Position(t, broker, acc.ID, 10, "50.00") // cash 500.00
	order, err := broker.PlaceOrder(t.Context(), acc.ID, builders.LimitSell(sym, 10, "55.00"))
	check.NoError(t, err, "place resting limit sell")
	check.Equal(t, "status", order.Status, "new")

	_, err = broker.SetPrice(t.Context(), sym, "55.00")
	check.NoError(t, err, "move price to limit")

	got, err := broker.GetOrder(t.Context(), acc.ID, order.ID)
	check.NoError(t, err, "get order")
	check.Equal(t, "status", got.Status, "filled")
	after, err := broker.GetAccount(t.Context(), acc.ID)
	check.NoError(t, err, "get account")
	check.Equal(t, "cash", after.Cash, money.Of(t, "500.00").Add(money.Of(t, "55.00").Mul(10)).String())
	_, err = broker.Position(t.Context(), acc.ID, sym)
	check.APIError(t, err, http.StatusNotFound, "position_not_found")
}

func TestOpenSellOrders_ReduceQtyAvailableForSale(t *testing.T) {
	t.Parallel()
	broker := fixtures.Broker(t)
	acc := fixtures.FundedAccount(t, broker, "1000.00")
	sym := fixtures.Position(t, broker, acc.ID, 10, "50.00")
	_, err := broker.PlaceOrder(t.Context(), acc.ID, builders.LimitSell(sym, 7, "80.00"))
	check.NoError(t, err, "rest a limit sell for 7 of 10")

	_, err = broker.PlaceOrder(t.Context(), acc.ID, builders.MarketSell(sym, 4))
	check.APIError(t, err, http.StatusForbidden, "insufficient_qty")

	_, err = broker.PlaceOrder(t.Context(), acc.ID, builders.MarketSell(sym, 3))
	check.NoError(t, err, "sell the remaining 3")
}

func TestReservedBuyingPower_BlocksFurtherBuys(t *testing.T) {
	t.Parallel()
	broker := fixtures.Broker(t)
	acc := fixtures.FundedAccount(t, broker, "1000.00")
	sym := fixtures.Symbol(t, broker, "100.00")
	_, err := broker.PlaceOrder(t.Context(), acc.ID, builders.LimitBuy(sym, 9, "100.00"))
	check.NoError(t, err, "spend 900.00")
	_, err = broker.PlaceOrder(t.Context(), acc.ID, builders.LimitBuy(sym, 1, "99.00"))
	check.NoError(t, err, "reserve 99.00")

	_, err = broker.PlaceOrder(t.Context(), acc.ID, builders.MarketBuy(sym, 1))

	check.APIError(t, err, http.StatusForbidden, "insufficient_buying_power")
}

func TestCancelOrder_ReleasesReservation(t *testing.T) {
	t.Parallel()
	broker := fixtures.Broker(t)
	acc := fixtures.FundedAccount(t, broker, "1000.00")
	sym := fixtures.Symbol(t, broker, "100.00")
	order, err := broker.PlaceOrder(t.Context(), acc.ID, builders.LimitBuy(sym, 5, "90.00"))
	check.NoError(t, err, "place resting limit buy")

	check.NoError(t, broker.CancelOrder(t.Context(), acc.ID, order.ID), "cancel order")

	got, err := broker.GetOrder(t.Context(), acc.ID, order.ID)
	check.NoError(t, err, "get order")
	check.Equal(t, "status", got.Status, "canceled")
	check.True(t, got.CanceledAt != nil, "canceled_at must be set")
	after, err := broker.GetAccount(t.Context(), acc.ID)
	check.NoError(t, err, "get account")
	check.Equal(t, "buying_power", after.BuyingPower, "1000.00")

	// A canceled order must not fill later.
	_, err = broker.SetPrice(t.Context(), sym, "80.00")
	check.NoError(t, err, "move price through former limit")
	got, err = broker.GetOrder(t.Context(), acc.ID, order.ID)
	check.NoError(t, err, "get order")
	check.Equal(t, "status after price move", got.Status, "canceled")
}

func TestCancelOrder_NotOpen_NotCancelable(t *testing.T) {
	t.Parallel()
	broker := fixtures.Broker(t)
	acc := fixtures.FundedAccount(t, broker, "1000.00")
	sym := fixtures.Symbol(t, broker, "10.00")

	filled, err := broker.PlaceOrder(t.Context(), acc.ID, builders.MarketBuy(sym, 1))
	check.NoError(t, err, "filled order")
	err = broker.CancelOrder(t.Context(), acc.ID, filled.ID)
	check.APIError(t, err, http.StatusUnprocessableEntity, "order_not_cancelable")

	open, err := broker.PlaceOrder(t.Context(), acc.ID, builders.LimitBuy(sym, 1, "5.00"))
	check.NoError(t, err, "open order")
	check.NoError(t, broker.CancelOrder(t.Context(), acc.ID, open.ID), "first cancel")
	err = broker.CancelOrder(t.Context(), acc.ID, open.ID)
	check.APIError(t, err, http.StatusUnprocessableEntity, "order_not_cancelable")

	after, err := broker.GetAccount(t.Context(), acc.ID)
	check.NoError(t, err, "get account")
	check.Equal(t, "buying_power (released once, not twice)", after.BuyingPower, after.Cash)
}
