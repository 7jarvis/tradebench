package api_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/7jarvis/tradebench/tests/internal/builders"
	"github.com/7jarvis/tradebench/tests/internal/check"
	"github.com/7jarvis/tradebench/tests/internal/fixtures"
	"github.com/7jarvis/tradebench/tests/internal/models"
	"github.com/7jarvis/tradebench/tests/internal/money"
)

func TestMarketBuy_FillsAtCurrentPriceAndDebitsCash(t *testing.T) {
	t.Parallel()
	broker := fixtures.Broker(t)
	acc := fixtures.FundedAccount(t, broker, "10000.00")
	sym := fixtures.Symbol(t, broker, "190.00")

	order, err := broker.PlaceOrder(t.Context(), acc.ID, builders.MarketBuy(sym, 10))
	check.NoError(t, err, "place market buy")

	check.Equal(t, "status", order.Status, "filled")
	check.Equal(t, "filled_qty", order.FilledQty, "10")
	check.Equal(t, "filled_avg_price", check.Value(t, "filled_avg_price", order.FilledAvgPrice), "190.00")
	check.Equal(t, "client_order_id defaults to id", order.ClientOrderID, order.ID)

	cost := money.Of(t, "190.00").Mul(10)
	after, err := broker.GetAccount(t.Context(), acc.ID)
	check.NoError(t, err, "get account")
	check.Equal(t, "cash", after.Cash, money.Of(t, "10000.00").Sub(cost).String())
	check.Equal(t, "buying_power", after.BuyingPower, after.Cash)

	pos, err := broker.Position(t.Context(), acc.ID, sym)
	check.NoError(t, err, "get position")
	check.Equal(t, "position qty", pos.Qty, "10")
	check.Equal(t, "avg_entry_price", pos.AvgEntryPrice, "190.00")
	check.Equal(t, "cost_basis", pos.CostBasis, cost.String())
}

func TestMarketBuy_ExactlyAllBuyingPower_Fills(t *testing.T) {
	t.Parallel()
	broker := fixtures.Broker(t)
	acc := fixtures.FundedAccount(t, broker, "500.00")
	sym := fixtures.Symbol(t, broker, "100.00")

	_, err := broker.PlaceOrder(t.Context(), acc.ID, builders.MarketBuy(sym, 5))
	check.NoError(t, err, "buy for exactly the available buying power")

	after, err := broker.GetAccount(t.Context(), acc.ID)
	check.NoError(t, err, "get account")
	check.Equal(t, "cash", after.Cash, "0.00")
}

func TestMarketBuy_InsufficientBuyingPower_RejectedWithoutSideEffects(t *testing.T) {
	t.Parallel()
	broker := fixtures.Broker(t)
	acc := fixtures.FundedAccount(t, broker, "499.99")
	sym := fixtures.Symbol(t, broker, "100.00")

	_, err := broker.PlaceOrder(t.Context(), acc.ID, builders.MarketBuy(sym, 5))

	check.APIError(t, err, http.StatusForbidden, "insufficient_buying_power")
	after, err := broker.GetAccount(t.Context(), acc.ID)
	check.NoError(t, err, "get account")
	check.Equal(t, "cash unchanged", after.Cash, "499.99")
	orders, err := broker.ListOrders(t.Context(), acc.ID, "all")
	check.NoError(t, err, "list orders")
	check.Equal(t, "orders created", len(orders), 0)
}

func TestMarketSell_ReducesPositionAndCreditsCash(t *testing.T) {
	t.Parallel()
	broker := fixtures.Broker(t)
	acc := fixtures.FundedAccount(t, broker, "1000.00")
	sym := fixtures.Position(t, broker, acc.ID, 10, "50.00") // cash now 500.00
	_, err := broker.SetPrice(t.Context(), sym, "60.00")
	check.NoError(t, err, "move price up")

	_, err = broker.PlaceOrder(t.Context(), acc.ID, builders.MarketSell(sym, 4))
	check.NoError(t, err, "sell part of position")

	pos, err := broker.Position(t.Context(), acc.ID, sym)
	check.NoError(t, err, "get position")
	check.Equal(t, "remaining qty", pos.Qty, "6")
	check.Equal(t, "avg_entry_price unchanged by sell", pos.AvgEntryPrice, "50.00")

	after, err := broker.GetAccount(t.Context(), acc.ID)
	check.NoError(t, err, "get account")
	check.Equal(t, "cash", after.Cash, money.Of(t, "500.00").Add(money.Of(t, "60.00").Mul(4)).String())
}

func TestMarketSell_WholePosition_RemovesPosition(t *testing.T) {
	t.Parallel()
	broker := fixtures.Broker(t)
	acc := fixtures.FundedAccount(t, broker, "1000.00")
	sym := fixtures.Position(t, broker, acc.ID, 3, "10.00")

	_, err := broker.PlaceOrder(t.Context(), acc.ID, builders.MarketSell(sym, 3))
	check.NoError(t, err, "sell whole position")

	_, err = broker.Position(t.Context(), acc.ID, sym)
	check.APIError(t, err, http.StatusNotFound, "position_not_found")
	positions, err := broker.Positions(t.Context(), acc.ID)
	check.NoError(t, err, "list positions")
	check.Equal(t, "positions", len(positions), 0)
}

func TestSell_MoreThanHeld_InsufficientQty(t *testing.T) {
	t.Parallel()
	broker := fixtures.Broker(t)
	acc := fixtures.FundedAccount(t, broker, "1000.00")
	sym := fixtures.Position(t, broker, acc.ID, 3, "10.00")

	_, err := broker.PlaceOrder(t.Context(), acc.ID, builders.MarketSell(sym, 4))

	check.APIError(t, err, http.StatusForbidden, "insufficient_qty")
}

func TestSell_WithoutPosition_InsufficientQty(t *testing.T) {
	t.Parallel()
	broker := fixtures.Broker(t)
	acc := fixtures.FundedAccount(t, broker, "1000.00")
	sym := fixtures.Symbol(t, broker, "10.00")

	_, err := broker.PlaceOrder(t.Context(), acc.ID, builders.MarketSell(sym, 1))

	check.APIError(t, err, http.StatusForbidden, "insufficient_qty")
}

func TestPlaceOrder_UnknownSymbol(t *testing.T) {
	t.Parallel()
	broker := fixtures.Broker(t)
	acc := fixtures.FundedAccount(t, broker, "100.00")

	_, err := broker.PlaceOrder(t.Context(), acc.ID, builders.MarketBuy("NOSUCHSYM", 1))

	e := check.APIError(t, err, http.StatusUnprocessableEntity, "unknown_symbol")
	check.Equal(t, "field", e.Field, "symbol")
}

func TestPlaceOrder_FieldValidation(t *testing.T) {
	t.Parallel()
	limit := func(p string) *string { return &p }
	cases := map[string]struct {
		edit  func(*models.OrderRequest)
		field string
	}{
		"missing symbol":           {func(r *models.OrderRequest) { r.Symbol = "" }, "symbol"},
		"lowercase symbol":         {func(r *models.OrderRequest) { r.Symbol = "aapl" }, "symbol"},
		"unknown side":             {func(r *models.OrderRequest) { r.Side = "short" }, "side"},
		"unknown type":             {func(r *models.OrderRequest) { r.Type = "stop" }, "type"},
		"zero qty":                 {func(r *models.OrderRequest) { r.Qty = "0" }, "qty"},
		"negative qty":             {func(r *models.OrderRequest) { r.Qty = "-1" }, "qty"},
		"fractional qty":           {func(r *models.OrderRequest) { r.Qty = "1.5" }, "qty"},
		"qty above max":            {func(r *models.OrderRequest) { r.Qty = "1000001" }, "qty"},
		"limit without price":      {func(r *models.OrderRequest) { r.Type = "limit" }, "limit_price"},
		"market with limit price":  {func(r *models.OrderRequest) { r.LimitPrice = limit("1.00") }, "limit_price"},
		"zero limit price":         {func(r *models.OrderRequest) { r.Type, r.LimitPrice = "limit", limit("0") }, "limit_price"},
		"limit price with 3 dp":    {func(r *models.OrderRequest) { r.Type, r.LimitPrice = "limit", limit("1.001") }, "limit_price"},
		"client_order_id 49 chars": {func(r *models.OrderRequest) { r.ClientOrderID = strings.Repeat("x", 49) }, "client_order_id"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			broker := fixtures.Broker(t)
			acc := fixtures.FundedAccount(t, broker, "100.00")
			req := builders.MarketBuy("AAPL", 1)
			tc.edit(&req)

			_, err := broker.PlaceOrder(t.Context(), acc.ID, req)

			check.ValidationError(t, err, tc.field)
		})
	}
}

func TestPlaceOrder_ClientOrderIDUniquePerAccount(t *testing.T) {
	t.Parallel()
	broker := fixtures.Broker(t)
	first := fixtures.FundedAccount(t, broker, "1000.00")
	second := fixtures.FundedAccount(t, broker, "1000.00")
	sym := fixtures.Symbol(t, broker, "10.00")
	req := builders.MarketBuy(sym, 1)
	req.ClientOrderID = strings.Repeat("c", 48) // also the max length

	order, err := broker.PlaceOrder(t.Context(), first.ID, req)
	check.NoError(t, err, "first order")
	check.Equal(t, "client_order_id echoed", order.ClientOrderID, req.ClientOrderID)

	_, err = broker.PlaceOrder(t.Context(), first.ID, req)
	check.APIError(t, err, http.StatusConflict, "duplicate_client_order_id")

	_, err = broker.PlaceOrder(t.Context(), second.ID, req)
	check.NoError(t, err, "same client_order_id on another account")
}

func TestGetOrder_OfAnotherAccount_NotFound(t *testing.T) {
	t.Parallel()
	broker := fixtures.Broker(t)
	owner := fixtures.FundedAccount(t, broker, "100.00")
	stranger := fixtures.Account(t, broker)
	sym := fixtures.Symbol(t, broker, "10.00")
	order, err := broker.PlaceOrder(t.Context(), owner.ID, builders.MarketBuy(sym, 1))
	check.NoError(t, err, "place order")

	_, err = broker.GetOrder(t.Context(), stranger.ID, order.ID)
	check.APIError(t, err, http.StatusNotFound, "order_not_found")

	err = broker.CancelOrder(t.Context(), stranger.ID, order.ID)
	check.APIError(t, err, http.StatusNotFound, "order_not_found")
}

func TestListOrders_FiltersByStatus(t *testing.T) {
	t.Parallel()
	broker := fixtures.Broker(t)
	acc := fixtures.FundedAccount(t, broker, "1000.00")
	sym := fixtures.Symbol(t, broker, "10.00")
	filled, err := broker.PlaceOrder(t.Context(), acc.ID, builders.MarketBuy(sym, 1))
	check.NoError(t, err, "filled order")
	open, err := broker.PlaceOrder(t.Context(), acc.ID, builders.LimitBuy(sym, 1, "5.00"))
	check.NoError(t, err, "open order")

	ids := func(os []models.Order) []string {
		out := []string{}
		for _, o := range os {
			out = append(out, o.ID)
		}
		return out
	}
	for status, want := range map[string][]string{
		"":       {open.ID},
		"open":   {open.ID},
		"closed": {filled.ID},
		"all":    {open.ID, filled.ID}, // newest first
	} {
		got, err := broker.ListOrders(t.Context(), acc.ID, status)
		check.NoError(t, err, "list orders status="+status)
		check.DeepEqual(t, "orders with status="+status, ids(got), want)
	}

	_, err = broker.ListOrders(t.Context(), acc.ID, "pending")
	check.ValidationError(t, err, "status")
}
