// Package models types the API contract. Every response body is decoded
// strictly (see Decode): unknown fields, missing fields and invalid values
// fail as contract violations before any test assertion runs.
//
// Field rules mirror the public contract in docs/openapi.yaml: money is a
// string with exactly two decimals, quantities are whole-number strings,
// nullable fields are always present (as null when empty).
package models

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"time"
)

// ---------- requests ----------

type OpenAccountRequest struct {
	OwnerName string `json:"owner_name"`
}

type DepositRequest struct {
	Amount string `json:"amount"`
}

type OrderRequest struct {
	Symbol        string  `json:"symbol"`
	Side          string  `json:"side"`
	Type          string  `json:"type"`
	Qty           string  `json:"qty"`
	LimitPrice    *string `json:"limit_price,omitempty"`
	ClientOrderID string  `json:"client_order_id,omitempty"`
}

type SetPriceRequest struct {
	Price string `json:"price"`
}

// ---------- responses ----------

type Account struct {
	ID          string    `json:"id"`
	OwnerName   string    `json:"owner_name"`
	Status      string    `json:"status"`
	Currency    string    `json:"currency"`
	Cash        string    `json:"cash"`
	BuyingPower string    `json:"buying_power"`
	CreatedAt   time.Time `json:"created_at"`
}

func (a Account) Validate() error {
	return errors.Join(
		uuid("id", a.ID),
		nonEmpty("owner_name", a.OwnerName),
		oneOf("status", a.Status, "active", "closed"),
		oneOf("currency", a.Currency, "USD"),
		amount("cash", a.Cash),
		amount("buying_power", a.BuyingPower),
		notZero("created_at", a.CreatedAt),
	)
}

type Order struct {
	ID             string     `json:"id"`
	ClientOrderID  string     `json:"client_order_id"`
	AccountID      string     `json:"account_id"`
	Symbol         string     `json:"symbol"`
	Side           string     `json:"side"`
	Type           string     `json:"type"`
	Qty            string     `json:"qty"`
	LimitPrice     *string    `json:"limit_price"`
	Status         string     `json:"status"`
	FilledQty      string     `json:"filled_qty"`
	FilledAvgPrice *string    `json:"filled_avg_price"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	FilledAt       *time.Time `json:"filled_at"`
	CanceledAt     *time.Time `json:"canceled_at"`
}

func (o Order) Validate() error {
	errs := []error{
		uuid("id", o.ID),
		nonEmpty("client_order_id", o.ClientOrderID),
		uuid("account_id", o.AccountID),
		nonEmpty("symbol", o.Symbol),
		oneOf("side", o.Side, "buy", "sell"),
		oneOf("type", o.Type, "market", "limit"),
		quantity("qty", o.Qty),
		oneOf("status", o.Status, "new", "filled", "canceled"),
		quantity("filled_qty", o.FilledQty),
		notZero("created_at", o.CreatedAt),
		notZero("updated_at", o.UpdatedAt),
		optionalAmount("limit_price", o.LimitPrice),
		optionalAmount("filled_avg_price", o.FilledAvgPrice),
	}
	// Cross-field invariants: the state machine must be self-consistent.
	if o.Type == "limit" && o.LimitPrice == nil {
		errs = append(errs, errors.New("limit_price: required for limit orders"))
	}
	if o.Type == "market" && o.LimitPrice != nil {
		errs = append(errs, errors.New("limit_price: must be null for market orders"))
	}
	switch o.Status {
	case "filled":
		if o.FilledAt == nil || o.FilledAvgPrice == nil || o.FilledQty != o.Qty {
			errs = append(errs, errors.New("filled order must have filled_at, filled_avg_price and filled_qty == qty"))
		}
	case "new":
		if o.FilledAt != nil || o.CanceledAt != nil || o.FilledQty != "0" {
			errs = append(errs, errors.New("new order must not be filled or canceled"))
		}
	case "canceled":
		if o.CanceledAt == nil || o.FilledAt != nil {
			errs = append(errs, errors.New("canceled order must have canceled_at and no filled_at"))
		}
	}
	return errors.Join(errs...)
}

type Position struct {
	Symbol        string `json:"symbol"`
	Qty           string `json:"qty"`
	AvgEntryPrice string `json:"avg_entry_price"`
	CostBasis     string `json:"cost_basis"`
	CurrentPrice  string `json:"current_price"`
	MarketValue   string `json:"market_value"`
	UnrealizedPL  string `json:"unrealized_pl"`
}

func (p Position) Validate() error {
	return errors.Join(
		nonEmpty("symbol", p.Symbol),
		quantity("qty", p.Qty),
		amount("avg_entry_price", p.AvgEntryPrice),
		amount("cost_basis", p.CostBasis),
		amount("current_price", p.CurrentPrice),
		amount("market_value", p.MarketValue),
		signedAmount("unrealized_pl", p.UnrealizedPL),
	)
}

type Price struct {
	Symbol    string    `json:"symbol"`
	Price     string    `json:"price"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (p Price) Validate() error {
	return errors.Join(nonEmpty("symbol", p.Symbol), amount("price", p.Price), notZero("updated_at", p.UpdatedAt))
}

// APIError is the documented error body.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}

func (e APIError) Validate() error {
	return errors.Join(nonEmpty("code", e.Code), nonEmpty("message", e.Message))
}

// TradeUpdate is the Kafka event emitted on each order state change.
type TradeUpdate struct {
	Event     string    `json:"event"`
	Timestamp time.Time `json:"timestamp"`
	Price     *string   `json:"price,omitempty"`
	Qty       *string   `json:"qty,omitempty"`
	Order     Order     `json:"order"`
}

func (u TradeUpdate) Validate() error {
	errs := []error{
		oneOf("event", u.Event, "new", "fill", "canceled"),
		notZero("timestamp", u.Timestamp),
		optionalAmount("price", u.Price),
	}
	if u.Event == "fill" && (u.Price == nil || u.Qty == nil) {
		errs = append(errs, errors.New("fill event must carry price and qty"))
	}
	if err := u.Order.Validate(); err != nil {
		errs = append(errs, fmt.Errorf("order: %w", err))
	}
	return errors.Join(errs...)
}

// ---------- field rules ----------

var (
	uuidRe     = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	amountRe   = regexp.MustCompile(`^(0|[1-9][0-9]*)\.[0-9]{2}$`)
	signedRe   = regexp.MustCompile(`^-?(0|[1-9][0-9]*)\.[0-9]{2}$`)
	quantityRe = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)
)

func uuid(f, v string) error {
	if !uuidRe.MatchString(v) {
		return fmt.Errorf("%s: %q is not a lowercase UUID", f, v)
	}
	return nil
}

func amount(f, v string) error {
	if !amountRe.MatchString(v) {
		return fmt.Errorf("%s: %q is not a non-negative amount with 2 decimals", f, v)
	}
	return nil
}

func signedAmount(f, v string) error {
	if !signedRe.MatchString(v) {
		return fmt.Errorf("%s: %q is not an amount with 2 decimals", f, v)
	}
	return nil
}

func optionalAmount(f string, v *string) error {
	if v == nil {
		return nil
	}
	return amount(f, *v)
}

func quantity(f, v string) error {
	if !quantityRe.MatchString(v) {
		return fmt.Errorf("%s: %q is not a whole-number string", f, v)
	}
	return nil
}

func nonEmpty(f, v string) error {
	if v == "" {
		return fmt.Errorf("%s: must not be empty", f)
	}
	return nil
}

func oneOf(f, v string, allowed ...string) error {
	if !slices.Contains(allowed, v) {
		return fmt.Errorf("%s: %q not in %v", f, v, allowed)
	}
	return nil
}

func notZero(f string, t time.Time) error {
	if t.IsZero() {
		return fmt.Errorf("%s: must be set", f)
	}
	return nil
}
