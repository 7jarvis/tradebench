// Package broker holds the trading domain: accounts, orders, positions and
// sandbox prices. It knows nothing about HTTP; the api package maps its
// errors to status codes.
package broker

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/7jarvis/tradebench/service/internal/money"
)

const (
	AccountActive = "active"
	AccountClosed = "closed"

	OrderNew      = "new"
	OrderFilled   = "filled"
	OrderCanceled = "canceled"

	SideBuy  = "buy"
	SideSell = "sell"

	TypeMarket = "market"
	TypeLimit  = "limit"

	Currency = "USD"
)

// Qty is a whole number of shares, encoded as a JSON string ("10").
type Qty int64

func (q Qty) MarshalJSON() ([]byte, error) {
	return json.Marshal(strconv.FormatInt(int64(q), 10))
}

type Account struct {
	ID          string      `json:"id"`
	OwnerName   string      `json:"owner_name"`
	Status      string      `json:"status"`
	Currency    string      `json:"currency"`
	Cash        money.Cents `json:"cash"`
	BuyingPower money.Cents `json:"buying_power"`
	CreatedAt   time.Time   `json:"created_at"`
}

type Order struct {
	ID             string       `json:"id"`
	ClientOrderID  string       `json:"client_order_id"`
	AccountID      string       `json:"account_id"`
	Symbol         string       `json:"symbol"`
	Side           string       `json:"side"`
	Type           string       `json:"type"`
	Qty            Qty          `json:"qty"`
	LimitPrice     *money.Cents `json:"limit_price"`
	Status         string       `json:"status"`
	FilledQty      Qty          `json:"filled_qty"`
	FilledAvgPrice *money.Cents `json:"filled_avg_price"`
	CreatedAt      time.Time    `json:"created_at"`
	UpdatedAt      time.Time    `json:"updated_at"`
	FilledAt       *time.Time   `json:"filled_at"`
	CanceledAt     *time.Time   `json:"canceled_at"`
}

type Position struct {
	Symbol        string      `json:"symbol"`
	Qty           Qty         `json:"qty"`
	AvgEntryPrice money.Cents `json:"avg_entry_price"`
	CostBasis     money.Cents `json:"cost_basis"`
	CurrentPrice  money.Cents `json:"current_price"`
	MarketValue   money.Cents `json:"market_value"`
	UnrealizedPL  money.Cents `json:"unrealized_pl"`
}

type Price struct {
	Symbol    string      `json:"symbol"`
	Price     money.Cents `json:"price"`
	UpdatedAt time.Time   `json:"updated_at"`
}

// TradeUpdate is the event published to Kafka on every order state change.
// Shape follows the "trade updates" stream common to brokerage APIs.
type TradeUpdate struct {
	Event     string       `json:"event"` // new | fill | canceled
	Timestamp time.Time    `json:"timestamp"`
	Price     *money.Cents `json:"price,omitempty"` // fill only
	Qty       *Qty         `json:"qty,omitempty"`   // fill only
	Order     Order        `json:"order"`
}

// Inputs come from the API layer as raw strings and are validated here.

type OpenAccountInput struct {
	OwnerName string
}

type PlaceOrderInput struct {
	Symbol        string
	Side          string
	Type          string
	Qty           string
	LimitPrice    *string
	ClientOrderID string
}

// Kind classifies domain errors; the API layer maps kinds to HTTP statuses.
type Kind int

const (
	KindValidation    Kind = iota + 1 // input is malformed
	KindNotFound                      // entity does not exist
	KindForbidden                     // business rule forbids the action
	KindConflict                      // duplicate / state conflict
	KindUnprocessable                 // valid input, but not applicable to current state
)

type Error struct {
	Kind    Kind
	Code    string
	Message string
	Field   string
}

func (e *Error) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Field)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func invalid(field, msg string) *Error {
	return &Error{Kind: KindValidation, Code: "validation_error", Message: msg, Field: field}
}

var (
	ErrAccountNotFound  = &Error{Kind: KindNotFound, Code: "account_not_found", Message: "account not found"}
	ErrOrderNotFound    = &Error{Kind: KindNotFound, Code: "order_not_found", Message: "order not found"}
	ErrPositionNotFound = &Error{Kind: KindNotFound, Code: "position_not_found", Message: "position not found"}
	ErrSymbolNotFound   = &Error{Kind: KindNotFound, Code: "symbol_not_found", Message: "symbol not found"}
	ErrUnknownSymbol    = &Error{Kind: KindValidation, Code: "unknown_symbol", Message: "symbol is not tradable", Field: "symbol"}
	ErrAccountInactive  = &Error{Kind: KindForbidden, Code: "account_not_active", Message: "account is not active"}
	ErrBuyingPower      = &Error{Kind: KindForbidden, Code: "insufficient_buying_power", Message: "insufficient buying power"}
	ErrInsufficientQty  = &Error{Kind: KindForbidden, Code: "insufficient_qty", Message: "insufficient qty available for sale"}
	ErrDuplicateClient  = &Error{Kind: KindConflict, Code: "duplicate_client_order_id", Message: "client_order_id must be unique per account", Field: "client_order_id"}
	ErrNotCancelable    = &Error{Kind: KindUnprocessable, Code: "order_not_cancelable", Message: "only open orders can be canceled"}
)
