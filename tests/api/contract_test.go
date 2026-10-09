package api_test

import (
	"net/http"
	"testing"

	"github.com/7jarvis/tradebench/tests/internal/check"
	"github.com/7jarvis/tradebench/tests/internal/fixtures"
)

// The request contract is strict: a client bug (number instead of string,
// misspelled field) must be rejected loudly, never silently ignored. In a
// trading API a silently dropped "limit_price" turns a limit order into a
// market order.
func TestPlaceOrder_MalformedBody_Rejected400(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"qty as JSON number":      `{"symbol":"AAPL","side":"buy","type":"market","qty":10}`,
		"misspelled limit_price":  `{"symbol":"AAPL","side":"buy","type":"limit","qty":"1","limitPrice":"1.00"}`,
		"unknown field":           `{"symbol":"AAPL","side":"buy","type":"market","qty":"1","extended_hours":true}`,
		"empty body":              ``,
		"array instead of object": `[{"symbol":"AAPL"}]`,
		"two objects":             `{"symbol":"AAPL","side":"buy","type":"market","qty":"1"}{}`,
		"truncated JSON":          `{"symbol":"AAPL",`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			broker := fixtures.Broker(t)
			acc := fixtures.FundedAccount(t, broker, "1000.00")

			_, err := broker.PlaceOrderRawJSON(t.Context(), acc.ID, body)

			check.APIError(t, err, http.StatusBadRequest, "invalid_json")
			orders, err := broker.ListOrders(t.Context(), acc.ID, "all")
			check.NoError(t, err, "list orders")
			check.Equal(t, "orders created", len(orders), 0)
		})
	}
}
