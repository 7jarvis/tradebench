package api_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/7jarvis/tradebench/tests/internal/builders"
	"github.com/7jarvis/tradebench/tests/internal/check"
	"github.com/7jarvis/tradebench/tests/internal/fixtures"
	"github.com/7jarvis/tradebench/tests/internal/money"
)

func TestOpenAccount_StartsActiveWithZeroBalance(t *testing.T) {
	t.Parallel()
	broker := fixtures.Broker(t)

	acc := fixtures.Account(t, broker)

	check.Equal(t, "status", acc.Status, "active")
	check.Equal(t, "currency", acc.Currency, "USD")
	check.Equal(t, "cash", acc.Cash, "0.00")
	check.Equal(t, "buying_power", acc.BuyingPower, "0.00")

	got, err := broker.GetAccount(t.Context(), acc.ID)
	check.NoError(t, err, "get account")
	check.DeepEqual(t, "persisted account", got, acc)
}

func TestOpenAccount_TrimsOwnerName(t *testing.T) {
	t.Parallel()
	broker := fixtures.Broker(t)

	acc, err := broker.OpenAccount(t.Context(), "  Jane Doe  ")
	check.NoError(t, err, "open account")
	t.Cleanup(func() { _ = broker.CloseAccount(context.Background(), acc.ID) })

	check.Equal(t, "owner_name", acc.OwnerName, "Jane Doe")
}

func TestOpenAccount_OwnerNameValidation(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		name  string
		valid bool
	}{
		"empty":                    {"", false},
		"whitespace only":          {"   ", false},
		"100 chars (upper bound)":  {strings.Repeat("a", 100), true},
		"101 chars (above bound)":  {strings.Repeat("a", 101), false},
		"unicode counted as chars": {strings.Repeat("ж", 100), true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			broker := fixtures.Broker(t)

			acc, err := broker.OpenAccount(t.Context(), tc.name)

			if tc.valid {
				check.NoError(t, err, "open account")
				_ = broker.CloseAccount(t.Context(), acc.ID)
				return
			}
			check.ValidationError(t, err, "owner_name")
		})
	}
}

func TestGetAccount_UnknownOrMalformedID_NotFound(t *testing.T) {
	t.Parallel()
	broker := fixtures.Broker(t)

	for _, id := range []string{"00000000-0000-4000-8000-000000000000", "not-a-uuid", "1' OR '1'='1"} {
		_, err := broker.GetAccount(t.Context(), id)
		check.APIError(t, err, http.StatusNotFound, "account_not_found")
	}
}

func TestDeposit_IncreasesCashAndBuyingPower(t *testing.T) {
	t.Parallel()
	broker := fixtures.Broker(t)
	acc := fixtures.FundedAccount(t, broker, "1000.00")

	acc, err := broker.Deposit(t.Context(), acc.ID, "250.55")
	check.NoError(t, err, "second deposit")

	want := money.Of(t, "1000.00").Add(money.Of(t, "250.55")).String()
	check.Equal(t, "cash", acc.Cash, want)
	check.Equal(t, "buying_power", acc.BuyingPower, want)
}

func TestDeposit_AmountValidation(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		amount string
		valid  bool
	}{
		"zero":                       {"0", false},
		"negative":                   {"-1.00", false},
		"three decimals":             {"1.001", false},
		"not a number":               {"ten", false},
		"exponent":                   {"1e3", false},
		"one cent (lower bound)":     {"0.01", true},
		"max per deposit":            {"1000000.00", true},
		"one cent above max deposit": {"1000000.01", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			broker := fixtures.Broker(t)
			acc := fixtures.Account(t, broker)

			got, err := broker.Deposit(t.Context(), acc.ID, tc.amount)

			if tc.valid {
				check.NoError(t, err, "deposit")
				check.Equal(t, "cash", got.Cash, money.Of(t, tc.amount).String())
				return
			}
			check.ValidationError(t, err, "amount")
			after, err := broker.GetAccount(t.Context(), acc.ID)
			check.NoError(t, err, "get account")
			check.Equal(t, "cash after rejected deposit", after.Cash, "0.00")
		})
	}
}

func TestDeposit_ClosedAccount_Forbidden(t *testing.T) {
	t.Parallel()
	broker := fixtures.Broker(t)
	acc := fixtures.Account(t, broker)
	check.NoError(t, broker.CloseAccount(t.Context(), acc.ID), "close account")

	_, err := broker.Deposit(t.Context(), acc.ID, "10.00")

	check.APIError(t, err, http.StatusForbidden, "account_not_active")
}

func TestCloseAccount_CancelsOpenOrdersAndIsIdempotent(t *testing.T) {
	t.Parallel()
	broker := fixtures.Broker(t)
	acc := fixtures.FundedAccount(t, broker, "1000.00")
	sym := fixtures.Symbol(t, broker, "50.00")
	resting, err := broker.PlaceOrder(t.Context(), acc.ID, builders.LimitBuy(sym, 10, "40.00"))
	check.NoError(t, err, "place resting limit order")

	check.NoError(t, broker.CloseAccount(t.Context(), acc.ID), "close account")
	check.NoError(t, broker.CloseAccount(t.Context(), acc.ID), "close account again")

	closed, err := broker.GetAccount(t.Context(), acc.ID)
	check.NoError(t, err, "get account")
	check.Equal(t, "status", closed.Status, "closed")
	check.Equal(t, "buying_power (reservation released)", closed.BuyingPower, "1000.00")

	order, err := broker.GetOrder(t.Context(), acc.ID, resting.ID)
	check.NoError(t, err, "get order")
	check.Equal(t, "order status", order.Status, "canceled")

	_, err = broker.PlaceOrder(t.Context(), acc.ID, builders.MarketBuy(sym, 1))
	check.APIError(t, err, http.StatusForbidden, "account_not_active")
}
