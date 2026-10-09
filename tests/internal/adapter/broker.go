// Package adapter knows the API surface: which endpoint, which method, which
// headers, how the payload is serialized. It returns raw responses and makes
// no judgement about them — that is the service layer's job.
package adapter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/7jarvis/tradebench/tests/internal/httpclient"
	"github.com/7jarvis/tradebench/tests/internal/models"
)

type Broker struct {
	http  *httpclient.Client
	token string // empty => no Authorization header
}

func NewBroker(c *httpclient.Client, token string) *Broker {
	return &Broker{http: c, token: token}
}

// WithToken returns a copy that authenticates with another token ("" sends none).
func (a *Broker) WithToken(token string) *Broker {
	cp := *a
	cp.token = token
	return &cp
}

func (a *Broker) OpenAccount(ctx context.Context, req models.OpenAccountRequest) (*httpclient.Response, error) {
	return a.send(ctx, http.MethodPost, "/v1/accounts", nil, req)
}

func (a *Broker) GetAccount(ctx context.Context, id string) (*httpclient.Response, error) {
	return a.send(ctx, http.MethodGet, "/v1/accounts/"+seg(id), nil, nil)
}

func (a *Broker) CloseAccount(ctx context.Context, id string) (*httpclient.Response, error) {
	return a.send(ctx, http.MethodDelete, "/v1/accounts/"+seg(id), nil, nil)
}

func (a *Broker) Deposit(ctx context.Context, id string, req models.DepositRequest) (*httpclient.Response, error) {
	return a.send(ctx, http.MethodPost, "/v1/accounts/"+seg(id)+"/deposits", nil, req)
}

func (a *Broker) PlaceOrder(ctx context.Context, accountID string, req models.OrderRequest) (*httpclient.Response, error) {
	return a.send(ctx, http.MethodPost, "/v1/accounts/"+seg(accountID)+"/orders", nil, req)
}

// PlaceOrderRaw sends body as-is. Used only by contract tests that need a
// payload the typed request cannot express (wrong types, unknown fields).
func (a *Broker) PlaceOrderRaw(ctx context.Context, accountID string, body []byte) (*httpclient.Response, error) {
	return a.do(ctx, http.MethodPost, "/v1/accounts/"+seg(accountID)+"/orders", nil, body)
}

func (a *Broker) GetOrder(ctx context.Context, accountID, orderID string) (*httpclient.Response, error) {
	return a.send(ctx, http.MethodGet, "/v1/accounts/"+seg(accountID)+"/orders/"+seg(orderID), nil, nil)
}

func (a *Broker) ListOrders(ctx context.Context, accountID, status string) (*httpclient.Response, error) {
	var q url.Values
	if status != "" {
		q = url.Values{"status": {status}}
	}
	return a.send(ctx, http.MethodGet, "/v1/accounts/"+seg(accountID)+"/orders", q, nil)
}

func (a *Broker) CancelOrder(ctx context.Context, accountID, orderID string) (*httpclient.Response, error) {
	return a.send(ctx, http.MethodDelete, "/v1/accounts/"+seg(accountID)+"/orders/"+seg(orderID), nil, nil)
}

func (a *Broker) ListPositions(ctx context.Context, accountID string) (*httpclient.Response, error) {
	return a.send(ctx, http.MethodGet, "/v1/accounts/"+seg(accountID)+"/positions", nil, nil)
}

func (a *Broker) GetPosition(ctx context.Context, accountID, symbol string) (*httpclient.Response, error) {
	return a.send(ctx, http.MethodGet, "/v1/accounts/"+seg(accountID)+"/positions/"+seg(symbol), nil, nil)
}

func (a *Broker) GetPrice(ctx context.Context, symbol string) (*httpclient.Response, error) {
	return a.send(ctx, http.MethodGet, "/v1/market/prices/"+seg(symbol), nil, nil)
}

func (a *Broker) SetPrice(ctx context.Context, symbol string, req models.SetPriceRequest) (*httpclient.Response, error) {
	return a.send(ctx, http.MethodPut, "/v1/market/prices/"+seg(symbol), nil, req)
}

func (a *Broker) Ready(ctx context.Context) (*httpclient.Response, error) {
	return a.do(ctx, http.MethodGet, "/readyz", nil, nil)
}

func (a *Broker) send(ctx context.Context, method, path string, q url.Values, payload any) (*httpclient.Response, error) {
	var body []byte
	if payload != nil {
		var err error
		if body, err = json.Marshal(payload); err != nil {
			return nil, err
		}
	}
	return a.do(ctx, method, path, q, body)
}

func (a *Broker) do(ctx context.Context, method, path string, q url.Values, body []byte) (*httpclient.Response, error) {
	h := http.Header{"Accept": {"application/json"}}
	if body != nil {
		h.Set("Content-Type", "application/json")
	}
	if a.token != "" {
		h.Set("Authorization", "Bearer "+a.token)
	}
	return a.http.Do(ctx, httpclient.Request{Method: method, Path: path, Query: q, Header: h, Body: body})
}

func seg(s string) string { return url.PathEscape(s) }
