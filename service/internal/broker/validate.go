package broker

import (
	"crypto/rand"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/7jarvis/tradebench/service/internal/money"
)

const (
	maxOwnerNameLen     = 100
	maxClientOrderIDLen = 48
	maxQty              = 1_000_000
	maxDeposit          = money.Cents(1_000_000_00) // 1,000,000.00 per deposit
	maxPrice            = money.Cents(1_000_000_00)
)

var (
	uuidRe   = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	symbolRe = regexp.MustCompile(`^[A-Z]{1,10}$`)
)

// validOrder is PlaceOrderInput after parsing.
type validOrder struct {
	symbol        string
	side          string
	typ           string
	qty           int64
	limitPrice    *money.Cents
	clientOrderID string
}

func validateOrder(in PlaceOrderInput) (validOrder, error) {
	v := validOrder{symbol: in.Symbol, side: in.Side, typ: in.Type, clientOrderID: in.ClientOrderID}

	if v.symbol == "" {
		return v, invalid("symbol", "symbol is required")
	}
	if !symbolRe.MatchString(v.symbol) {
		return v, invalid("symbol", "symbol must be 1-10 uppercase letters")
	}
	if v.side != SideBuy && v.side != SideSell {
		return v, invalid("side", "side must be one of: buy, sell")
	}
	if v.typ != TypeMarket && v.typ != TypeLimit {
		return v, invalid("type", "type must be one of: market, limit")
	}

	qty, err := strconv.ParseInt(in.Qty, 10, 64)
	if err != nil || qty <= 0 || strings.HasPrefix(in.Qty, "+") {
		return v, invalid("qty", "qty must be a positive whole number")
	}
	if qty > maxQty {
		return v, invalid("qty", fmt.Sprintf("qty must not exceed %d", maxQty))
	}
	v.qty = qty

	switch {
	case v.typ == TypeLimit && in.LimitPrice == nil:
		return v, invalid("limit_price", "limit_price is required for limit orders")
	case v.typ == TypeMarket && in.LimitPrice != nil:
		return v, invalid("limit_price", "limit_price must not be set for market orders")
	case in.LimitPrice != nil:
		p, err := money.Parse(*in.LimitPrice)
		if err != nil || p == 0 || p > maxPrice {
			return v, invalid("limit_price", "limit_price must be a positive amount up to 1000000.00")
		}
		v.limitPrice = &p
	}

	if utf8.RuneCountInString(v.clientOrderID) > maxClientOrderIDLen {
		return v, invalid("client_order_id", fmt.Sprintf("client_order_id must not exceed %d characters", maxClientOrderIDLen))
	}
	return v, nil
}

func validateOwnerName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", invalid("owner_name", "owner_name is required")
	}
	if utf8.RuneCountInString(name) > maxOwnerNameLen {
		return "", invalid("owner_name", fmt.Sprintf("owner_name must not exceed %d characters", maxOwnerNameLen))
	}
	return name, nil
}

func validateAmount(field, s string, max money.Cents) (money.Cents, error) {
	v, err := money.Parse(s)
	if err != nil || v == 0 {
		return 0, invalid(field, field+" must be a positive amount with at most 2 decimals")
	}
	if v > max {
		return 0, invalid(field, fmt.Sprintf("%s must not exceed %s", field, max))
	}
	return v, nil
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
