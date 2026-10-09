// Package service is the layer tests talk to. Methods are business actions
// in domain language ("place an order", "deposit"), return typed models and
// hide HTTP. A documented API error comes back as *APIError, anything
// undocumented (5xx, wrong status, broken contract) as a plain error, so a
// test can never mistake a server bug for an expected rejection.
package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/7jarvis/tradebench/tests/internal/adapter"
	"github.com/7jarvis/tradebench/tests/internal/httpclient"
	"github.com/7jarvis/tradebench/tests/internal/models"
)

// APIError is an expected, documented business rejection.
type APIError struct {
	Status    int
	RequestID string
	models.APIError
}

func (e *APIError) Error() string {
	s := fmt.Sprintf("api error %d %s: %s", e.Status, e.Code, e.Message)
	if e.Field != "" {
		s += " (field " + e.Field + ")"
	}
	return s + " [request " + e.RequestID + "]"
}

// AsAPIError unwraps err into *APIError.
func AsAPIError(err error) (*APIError, bool) {
	var e *APIError
	ok := errors.As(err, &e)
	return e, ok
}

type Broker struct {
	api *adapter.Broker
}

func NewBroker(a *adapter.Broker) *Broker { return &Broker{api: a} }

// ---------- accounts ----------

func (b *Broker) OpenAccount(ctx context.Context, ownerName string) (models.Account, error) {
	return one[models.Account](b.api.OpenAccount(ctx, models.OpenAccountRequest{OwnerName: ownerName}))(http.StatusCreated)
}

func (b *Broker) GetAccount(ctx context.Context, id string) (models.Account, error) {
	return one[models.Account](b.api.GetAccount(ctx, id))(http.StatusOK)
}

func (b *Broker) CloseAccount(ctx context.Context, id string) error {
	return none(b.api.CloseAccount(ctx, id))
}

func (b *Broker) Deposit(ctx context.Context, id, amount string) (models.Account, error) {
	return one[models.Account](b.api.Deposit(ctx, id, models.DepositRequest{Amount: amount}))(http.StatusOK)
}

// ---------- orders ----------

func (b *Broker) PlaceOrder(ctx context.Context, accountID string, req models.OrderRequest) (models.Order, error) {
	return one[models.Order](b.api.PlaceOrder(ctx, accountID, req))(http.StatusCreated)
}

// PlaceOrderRawJSON is for contract tests only: the body is sent unmodified.
func (b *Broker) PlaceOrderRawJSON(ctx context.Context, accountID, body string) (models.Order, error) {
	return one[models.Order](b.api.PlaceOrderRaw(ctx, accountID, []byte(body)))(http.StatusCreated)
}

func (b *Broker) GetOrder(ctx context.Context, accountID, orderID string) (models.Order, error) {
	return one[models.Order](b.api.GetOrder(ctx, accountID, orderID))(http.StatusOK)
}

// ListOrders: status is "open", "closed", "all" or "" (server default: open).
func (b *Broker) ListOrders(ctx context.Context, accountID, status string) ([]models.Order, error) {
	return list[models.Order](b.api.ListOrders(ctx, accountID, status))
}

func (b *Broker) CancelOrder(ctx context.Context, accountID, orderID string) error {
	return none(b.api.CancelOrder(ctx, accountID, orderID))
}

// ---------- positions ----------

func (b *Broker) Positions(ctx context.Context, accountID string) ([]models.Position, error) {
	return list[models.Position](b.api.ListPositions(ctx, accountID))
}

func (b *Broker) Position(ctx context.Context, accountID, symbol string) (models.Position, error) {
	return one[models.Position](b.api.GetPosition(ctx, accountID, symbol))(http.StatusOK)
}

// ---------- sandbox market ----------

func (b *Broker) Price(ctx context.Context, symbol string) (models.Price, error) {
	return one[models.Price](b.api.GetPrice(ctx, symbol))(http.StatusOK)
}

// SetPrice moves the sandbox price; open limit orders the price crosses fill.
func (b *Broker) SetPrice(ctx context.Context, symbol, price string) (models.Price, error) {
	return one[models.Price](b.api.SetPrice(ctx, symbol, models.SetPriceRequest{Price: price}))(http.StatusOK)
}

func (b *Broker) Ready(ctx context.Context) error {
	resp, err := b.api.Ready(ctx)
	if err != nil {
		return err
	}
	if resp.Status != http.StatusOK {
		return fmt.Errorf("readyz returned %d", resp.Status)
	}
	return nil
}

// ---------- response handling ----------

// one returns a function so call sites read as one[T](call)(expectedStatus).
func one[T models.Validatable](resp *httpclient.Response, err error) func(int) (T, error) {
	return func(want int) (T, error) {
		var zero T
		if err != nil {
			return zero, err
		}
		if resp.Status != want {
			return zero, errorFrom(resp, want)
		}
		return models.Decode[T](resp.Body)
	}
}

func list[T models.Validatable](resp *httpclient.Response, err error) ([]T, error) {
	if err != nil {
		return nil, err
	}
	if resp.Status != http.StatusOK {
		return nil, errorFrom(resp, http.StatusOK)
	}
	return models.DecodeList[T](resp.Body)
}

func none(resp *httpclient.Response, err error) error {
	if err != nil {
		return err
	}
	if resp.Status != http.StatusNoContent {
		return errorFrom(resp, http.StatusNoContent)
	}
	if len(resp.Body) != 0 {
		return fmt.Errorf("204 response must have an empty body, got %q", resp.Body)
	}
	return nil
}

// errorFrom turns an unexpected status into *APIError when it is a
// documented 4xx with a valid error body, otherwise into a plain error.
func errorFrom(resp *httpclient.Response, want int) error {
	if resp.Status >= 400 && resp.Status < 500 {
		body, err := models.Decode[models.APIError](resp.Body)
		if err != nil {
			return fmt.Errorf("status %d with undocumented error body: %w", resp.Status, err)
		}
		return &APIError{Status: resp.Status, RequestID: resp.RequestID, APIError: body}
	}
	return fmt.Errorf("unexpected status %d (want %d) [request %s]: %s", resp.Status, want, resp.RequestID, resp.Body)
}
