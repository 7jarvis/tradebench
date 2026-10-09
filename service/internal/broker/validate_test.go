package broker

import (
	"errors"
	"strings"
	"testing"
)

func ptr(s string) *string { return &s }

func TestValidateOrder(t *testing.T) {
	base := PlaceOrderInput{Symbol: "AAPL", Side: "buy", Type: "market", Qty: "10"}

	cases := []struct {
		name  string
		edit  func(*PlaceOrderInput)
		field string // "" means valid
	}{
		{"valid market", func(*PlaceOrderInput) {}, ""},
		{"valid limit", func(in *PlaceOrderInput) { in.Type = "limit"; in.LimitPrice = ptr("150.25") }, ""},
		{"missing symbol", func(in *PlaceOrderInput) { in.Symbol = "" }, "symbol"},
		{"lowercase symbol", func(in *PlaceOrderInput) { in.Symbol = "aapl" }, "symbol"},
		{"bad side", func(in *PlaceOrderInput) { in.Side = "short" }, "side"},
		{"bad type", func(in *PlaceOrderInput) { in.Type = "stop" }, "type"},
		{"zero qty", func(in *PlaceOrderInput) { in.Qty = "0" }, "qty"},
		{"fractional qty", func(in *PlaceOrderInput) { in.Qty = "1.5" }, "qty"},
		{"qty above max", func(in *PlaceOrderInput) { in.Qty = "1000001" }, "qty"},
		{"qty at max", func(in *PlaceOrderInput) { in.Qty = "1000000" }, ""},
		{"limit without price", func(in *PlaceOrderInput) { in.Type = "limit" }, "limit_price"},
		{"market with price", func(in *PlaceOrderInput) { in.LimitPrice = ptr("1.00") }, "limit_price"},
		{"limit price 3 decimals", func(in *PlaceOrderInput) { in.Type = "limit"; in.LimitPrice = ptr("1.001") }, "limit_price"},
		{"client id at limit", func(in *PlaceOrderInput) { in.ClientOrderID = strings.Repeat("a", 48) }, ""},
		{"client id too long", func(in *PlaceOrderInput) { in.ClientOrderID = strings.Repeat("a", 49) }, "client_order_id"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := base
			tc.edit(&in)
			_, err := validateOrder(in)
			if tc.field == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			var e *Error
			if !errors.As(err, &e) || e.Code != "validation_error" || e.Field != tc.field {
				t.Fatalf("want validation_error on %q, got %v", tc.field, err)
			}
		})
	}
}

func TestNewIDIsUUIDv4(t *testing.T) {
	for range 100 {
		if id := newID(); !uuidRe.MatchString(id) || id[14] != '4' {
			t.Fatalf("bad uuid %q", id)
		}
	}
}
