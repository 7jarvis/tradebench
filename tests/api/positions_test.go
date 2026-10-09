package api_test

import (
	"net/http"
	"testing"

	"github.com/7jarvis/tradebench/tests/internal/builders"
	"github.com/7jarvis/tradebench/tests/internal/check"
	"github.com/7jarvis/tradebench/tests/internal/fixtures"
	"github.com/7jarvis/tradebench/tests/internal/money"
)

func TestPosition_WeightedAverageEntryPriceAcrossBuys(t *testing.T) {
	t.Parallel()
	broker := fixtures.Broker(t)
	acc := fixtures.FundedAccount(t, broker, "10000.00")
	sym := fixtures.Position(t, broker, acc.ID, 10, "100.00")
	_, err := broker.SetPrice(t.Context(), sym, "130.00")
	check.NoError(t, err, "move price")
	_, err = broker.PlaceOrder(t.Context(), acc.ID, builders.MarketBuy(sym, 5))
	check.NoError(t, err, "second buy")

	pos, err := broker.Position(t.Context(), acc.ID, sym)
	check.NoError(t, err, "get position")

	cost := money.Of(t, "100.00").Mul(10).Add(money.Of(t, "130.00").Mul(5))
	value := money.Of(t, "130.00").Mul(15)
	check.Equal(t, "qty", pos.Qty, "15")
	check.Equal(t, "avg_entry_price", pos.AvgEntryPrice, cost.DivRound(15).String())
	check.Equal(t, "cost_basis", pos.CostBasis, cost.String())
	check.Equal(t, "current_price", pos.CurrentPrice, "130.00")
	check.Equal(t, "market_value", pos.MarketValue, value.String())
	check.Equal(t, "unrealized_pl", pos.UnrealizedPL, value.Sub(cost).String())
}

func TestPosition_UnrealizedLossIsNegative(t *testing.T) {
	t.Parallel()
	broker := fixtures.Broker(t)
	acc := fixtures.FundedAccount(t, broker, "1000.00")
	sym := fixtures.Position(t, broker, acc.ID, 4, "50.00")

	_, err := broker.SetPrice(t.Context(), sym, "47.25")
	check.NoError(t, err, "price falls")

	pos, err := broker.Position(t.Context(), acc.ID, sym)
	check.NoError(t, err, "get position")
	check.Equal(t, "unrealized_pl", pos.UnrealizedPL, money.Of(t, "-2.75").Mul(4).String())
}

func TestPositions_NewAccount_EmptyList(t *testing.T) {
	t.Parallel()
	broker := fixtures.Broker(t)
	acc := fixtures.Account(t, broker)

	positions, err := broker.Positions(t.Context(), acc.ID) // decoder rejects null
	check.NoError(t, err, "list positions")
	check.Equal(t, "len", len(positions), 0)

	_, err = broker.Position(t.Context(), acc.ID, "AAPL")
	check.APIError(t, err, http.StatusNotFound, "position_not_found")
}
