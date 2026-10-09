// Package httpclient is the lowest layer: it sends a request and returns the
// raw response. It knows nothing about endpoints or payloads, so swapping
// net/http for another transport only touches this file.
package httpclient

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Logger receives one line per request and per response. *testing.T
// satisfies it, so traffic is attached to the test that produced it and
// shown only when the test fails (or with -v).
type Logger interface {
	Logf(format string, args ...any)
}

type Request struct {
	Method string
	Path   string
	Query  url.Values
	Header http.Header
	Body   []byte
}

type Response struct {
	Status    int
	Header    http.Header
	Body      []byte
	Duration  time.Duration
	RequestID string
}

type Client struct {
	baseURL string
	http    *http.Client
	log     Logger
}

type Option func(*Client)

func WithLogger(l Logger) Option { return func(c *Client) { c.log = l } }

func WithTimeout(d time.Duration) Option { return func(c *Client) { c.http.Timeout = d } }

func New(baseURL string, opts ...Option) *Client {
	c := &Client{baseURL: baseURL, http: &http.Client{Timeout: 10 * time.Second}}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Do sends the request. Every request carries a fresh X-Request-Id so a
// failing test can be matched to the service log line.
func (c *Client) Do(ctx context.Context, r Request) (*Response, error) {
	u := c.baseURL + r.Path
	if len(r.Query) > 0 {
		u += "?" + r.Query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, r.Method, u, bytes.NewReader(r.Body))
	if err != nil {
		return nil, err
	}
	for k, vs := range r.Header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	reqID := newRequestID()
	req.Header.Set("X-Request-Id", reqID)

	c.logf("→ %s %s [%s] %s", r.Method, r.Path+query(r.Query), reqID, truncate(r.Body))

	start := time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		c.logf("✗ %s %s [%s] transport error: %v", r.Method, r.Path, reqID, err)
		return nil, fmt.Errorf("%s %s: %w", r.Method, r.Path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%s %s: read body: %w", r.Method, r.Path, err)
	}
	out := &Response{Status: resp.StatusCode, Header: resp.Header, Body: body, Duration: time.Since(start), RequestID: reqID}

	c.logf("← %d %s %s (%dms) %s", out.Status, r.Method, r.Path, out.Duration.Milliseconds(), truncate(body))
	return out, nil
}

func (c *Client) logf(format string, args ...any) {
	if c.log != nil {
		c.log.Logf(format, args...)
	}
}

func query(q url.Values) string {
	if len(q) == 0 {
		return ""
	}
	return "?" + q.Encode()
}

func truncate(b []byte) string {
	const max = 2048
	if len(b) > max {
		return string(b[:max]) + "…(truncated)"
	}
	return string(bytes.TrimSpace(b))
}

func newRequestID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "qa-" + hex.EncodeToString(b[:])
}
