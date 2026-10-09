// Package api exposes the broker service over HTTP/JSON.
package api

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/7jarvis/tradebench/service/internal/broker"
)

const maxBodyBytes = 1 << 20

type Server struct {
	svc   *broker.Service
	db    *sql.DB
	token string
	log   *slog.Logger
}

func NewHandler(svc *broker.Service, db *sql.DB, token string, log *slog.Logger) http.Handler {
	s := &Server{svc: svc, db: db, token: token, log: log}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /readyz", s.readyz)

	mux.HandleFunc("POST /v1/accounts", s.openAccount)
	mux.HandleFunc("GET /v1/accounts/{id}", s.getAccount)
	mux.HandleFunc("DELETE /v1/accounts/{id}", s.closeAccount)
	mux.HandleFunc("POST /v1/accounts/{id}/deposits", s.deposit)

	mux.HandleFunc("POST /v1/accounts/{id}/orders", s.placeOrder)
	mux.HandleFunc("GET /v1/accounts/{id}/orders", s.listOrders)
	mux.HandleFunc("GET /v1/accounts/{id}/orders/{order_id}", s.getOrder)
	mux.HandleFunc("DELETE /v1/accounts/{id}/orders/{order_id}", s.cancelOrder)

	mux.HandleFunc("GET /v1/accounts/{id}/positions", s.listPositions)
	mux.HandleFunc("GET /v1/accounts/{id}/positions/{symbol}", s.getPosition)

	mux.HandleFunc("GET /v1/market/prices/{symbol}", s.getPrice)
	mux.HandleFunc("PUT /v1/market/prices/{symbol}", s.setPrice)

	return s.recoverer(s.requestID(s.logging(s.auth(mux))))
}

// ---------- handlers ----------

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Second)
	defer cancel()
	if err := s.db.PingContext(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

type openAccountRequest struct {
	OwnerName string `json:"owner_name"`
}

func (s *Server) openAccount(w http.ResponseWriter, r *http.Request) {
	var req openAccountRequest
	if !s.decode(w, r, &req) {
		return
	}
	a, err := s.svc.OpenAccount(r.Context(), broker.OpenAccountInput{OwnerName: req.OwnerName})
	s.respond(w, r, http.StatusCreated, a, err)
}

func (s *Server) getAccount(w http.ResponseWriter, r *http.Request) {
	a, err := s.svc.GetAccount(r.Context(), r.PathValue("id"))
	s.respond(w, r, http.StatusOK, a, err)
}

func (s *Server) closeAccount(w http.ResponseWriter, r *http.Request) {
	err := s.svc.CloseAccount(r.Context(), r.PathValue("id"))
	s.respond(w, r, http.StatusNoContent, nil, err)
}

type depositRequest struct {
	Amount string `json:"amount"`
}

func (s *Server) deposit(w http.ResponseWriter, r *http.Request) {
	var req depositRequest
	if !s.decode(w, r, &req) {
		return
	}
	a, err := s.svc.Deposit(r.Context(), r.PathValue("id"), req.Amount)
	s.respond(w, r, http.StatusOK, a, err)
}

// All numeric fields are strings on the wire; a JSON number is a contract
// violation and is rejected with 400 by the strict decoder.
type placeOrderRequest struct {
	Symbol        string  `json:"symbol"`
	Side          string  `json:"side"`
	Type          string  `json:"type"`
	Qty           string  `json:"qty"`
	LimitPrice    *string `json:"limit_price"`
	ClientOrderID string  `json:"client_order_id"`
}

func (s *Server) placeOrder(w http.ResponseWriter, r *http.Request) {
	var req placeOrderRequest
	if !s.decode(w, r, &req) {
		return
	}
	o, err := s.svc.PlaceOrder(r.Context(), r.PathValue("id"), broker.PlaceOrderInput{
		Symbol: req.Symbol, Side: req.Side, Type: req.Type, Qty: req.Qty,
		LimitPrice: req.LimitPrice, ClientOrderID: req.ClientOrderID,
	})
	s.respond(w, r, http.StatusCreated, o, err)
}

func (s *Server) listOrders(w http.ResponseWriter, r *http.Request) {
	orders, err := s.svc.ListOrders(r.Context(), r.PathValue("id"), r.URL.Query().Get("status"))
	s.respond(w, r, http.StatusOK, orders, err)
}

func (s *Server) getOrder(w http.ResponseWriter, r *http.Request) {
	o, err := s.svc.GetOrder(r.Context(), r.PathValue("id"), r.PathValue("order_id"))
	s.respond(w, r, http.StatusOK, o, err)
}

func (s *Server) cancelOrder(w http.ResponseWriter, r *http.Request) {
	err := s.svc.CancelOrder(r.Context(), r.PathValue("id"), r.PathValue("order_id"))
	s.respond(w, r, http.StatusNoContent, nil, err)
}

func (s *Server) listPositions(w http.ResponseWriter, r *http.Request) {
	p, err := s.svc.ListPositions(r.Context(), r.PathValue("id"))
	s.respond(w, r, http.StatusOK, p, err)
}

func (s *Server) getPosition(w http.ResponseWriter, r *http.Request) {
	p, err := s.svc.GetPosition(r.Context(), r.PathValue("id"), r.PathValue("symbol"))
	s.respond(w, r, http.StatusOK, p, err)
}

func (s *Server) getPrice(w http.ResponseWriter, r *http.Request) {
	p, err := s.svc.GetPrice(r.Context(), r.PathValue("symbol"))
	s.respond(w, r, http.StatusOK, p, err)
}

type setPriceRequest struct {
	Price string `json:"price"`
}

func (s *Server) setPrice(w http.ResponseWriter, r *http.Request) {
	var req setPriceRequest
	if !s.decode(w, r, &req) {
		return
	}
	p, err := s.svc.SetPrice(r.Context(), r.PathValue("symbol"), req.Price)
	s.respond(w, r, http.StatusOK, p, err)
}

// ---------- encoding ----------

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}

// decode is strict: unknown fields, wrong types, trailing data and empty
// bodies are rejected with 400 invalid_json.
func (s *Server) decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	err := dec.Decode(dst)
	if err == nil && dec.Decode(&struct{}{}) != io.EOF {
		err = errors.New("body must contain a single JSON object")
	}
	if err != nil {
		msg := err.Error()
		if errors.Is(err, io.EOF) {
			msg = "request body is empty"
		}
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) {
			msg = fmt.Sprintf("field %q must be of type %s", typeErr.Field, typeErr.Type)
		}
		writeJSON(w, http.StatusBadRequest, errorBody{Code: "invalid_json", Message: msg})
		return false
	}
	return true
}

func (s *Server) respond(w http.ResponseWriter, r *http.Request, status int, body any, err error) {
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if status == http.StatusNoContent {
		w.WriteHeader(status)
		return
	}
	writeJSON(w, status, body)
}

func (s *Server) writeError(w http.ResponseWriter, r *http.Request, err error) {
	var de *broker.Error
	if !errors.As(err, &de) {
		s.log.ErrorContext(r.Context(), "internal error", "err", err, "request_id", w.Header().Get("X-Request-Id"))
		writeJSON(w, http.StatusInternalServerError, errorBody{Code: "internal_error", Message: "internal error"})
		return
	}
	status := map[broker.Kind]int{
		broker.KindValidation:    http.StatusUnprocessableEntity,
		broker.KindNotFound:      http.StatusNotFound,
		broker.KindForbidden:     http.StatusForbidden,
		broker.KindConflict:      http.StatusConflict,
		broker.KindUnprocessable: http.StatusUnprocessableEntity,
	}[de.Kind]
	writeJSON(w, status, errorBody{Code: de.Code, Message: de.Message, Field: de.Field})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// ---------- middleware ----------

func (s *Server) auth(next http.Handler) http.Handler {
	expected := []byte("Bearer " + s.token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			next.ServeHTTP(w, r)
			return
		}
		got := []byte(r.Header.Get("Authorization"))
		if subtle.ConstantTimeCompare(got, expected) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="tradebench"`)
			writeJSON(w, http.StatusUnauthorized, errorBody{Code: "unauthorized", Message: "missing or invalid bearer token"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.Header.Get("X-Request-Id"))
		if id == "" || len(id) > 64 {
			id = randomID()
		}
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (s *Server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			return
		}
		s.log.InfoContext(r.Context(), "request",
			"method", r.Method, "route", r.Pattern, "status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(), "request_id", w.Header().Get("X-Request-Id"))
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				s.log.ErrorContext(r.Context(), "panic", "value", v)
				writeJSON(w, http.StatusInternalServerError, errorBody{Code: "internal_error", Message: "internal error"})
			}
		}()
		next.ServeHTTP(w, r)
	})
}
