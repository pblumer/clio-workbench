// Package clio is a thin HTTP client against a running Clio instance's public
// API (see docs/WORKBENCH.md §2 principle 3). Its job in Stufe 0.5 is an
// honest connection status: not "is CLIO_URL configured?" but "is Clio
// actually reachable and does it accept our token?".
//
// The probe uses the lightweight authenticated read operation
// GET /api/v1/read-event-types, so a single request verifies both
// reachability AND the bearer token. Clio's pure health endpoint
// (GET /api/v1/ping) is unauthenticated and would not exercise the token,
// so it is intentionally not used as the sole probe.
//
// The token is held server-side and never leaves this process (§2 principle 4).
package clio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Status is the distilled connection state reported to the UI.
type Status string

const (
	// StatusOnline means Clio is reachable and accepted the token (2xx).
	StatusOnline Status = "online"
	// StatusUnauthorized means Clio is reachable but rejected the token
	// (HTTP 401/403).
	StatusUnauthorized Status = "unauthorized"
	// StatusUnreachable means no usable HTTP response was obtained: a
	// network/DNS/connection error, a timeout, or an unexpected status.
	StatusUnreachable Status = "unreachable"
	// StatusOffline means no CLIO_URL is configured; the Workbench runs
	// offline on the draft.
	StatusOffline Status = "offline"
)

// probePath is the authenticated read op used to verify reachability and token
// in one request (see package doc and docs/WORKBENCH.md §6.1).
const probePath = "/api/v1/read-event-types"

// defaultTimeout bounds a single connection probe.
const defaultTimeout = 5 * time.Second

// maxDrain caps how much of the probe response body we read so the connection
// can be reused without slurping a potentially long NDJSON stream.
const maxDrain = 4 << 10

// Result is the outcome of a connection check.
type Result struct {
	// Status is the distilled state.
	Status Status
	// Detail is a human-readable explanation (HTTP status, error text). It is
	// safe to render: it never contains the token.
	Detail string
	// Latency is the measured round-trip time of the probe request. Zero when
	// no request was made (offline) or it failed before being sent.
	Latency time.Duration
	// CheckedAt is when the check ran.
	CheckedAt time.Time
}

// OK reports whether the upstream is online.
func (r Result) OK() bool { return r.Status == StatusOnline }

// Client probes a Clio instance. It is safe for concurrent use.
type Client struct {
	baseURL string
	token   string
	httpc   *http.Client
}

// Option customises a Client.
type Option func(*Client)

// WithHTTPClient overrides the underlying HTTP client (used in tests).
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) {
		if h != nil {
			c.httpc = h
		}
	}
}

// WithTimeout sets the per-probe timeout on the default HTTP client.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		if d > 0 {
			c.httpc.Timeout = d
		}
	}
}

// New builds a Client for the given Clio base URL and bearer token. An empty
// baseURL yields a client that always reports StatusOffline.
func New(baseURL, token string, opts ...Option) *Client {
	c := &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		httpc:   &http.Client{Timeout: defaultTimeout},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Configured reports whether an upstream Clio URL is set.
func (c *Client) Configured() bool { return c.baseURL != "" }

// CheckConnection probes Clio and maps the outcome onto a Status. It never
// returns an error: every failure mode is expressed as a Result so callers can
// render it directly. Honour the caller's context for cancellation/deadline.
func (c *Client) CheckConnection(ctx context.Context) Result {
	now := time.Now()
	if c.baseURL == "" {
		return Result{
			Status:    StatusOffline,
			Detail:    "no CLIO_URL configured — drafting works offline",
			CheckedAt: now,
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+probePath, nil)
	if err != nil {
		return Result{Status: StatusUnreachable, Detail: err.Error(), CheckedAt: now}
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("Accept", "application/x-ndjson")

	start := time.Now()
	resp, err := c.httpc.Do(req)
	latency := time.Since(start)
	if err != nil {
		return Result{
			Status:    StatusUnreachable,
			Detail:    transportDetail(err),
			Latency:   latency,
			CheckedAt: now,
		}
	}
	defer resp.Body.Close()
	// Drain a bounded prefix so the connection can be reused, then discard.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxDrain))

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return Result{
			Status:    StatusOnline,
			Detail:    fmt.Sprintf("HTTP %d", resp.StatusCode),
			Latency:   latency,
			CheckedAt: now,
		}
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return Result{
			Status:    StatusUnauthorized,
			Detail:    fmt.Sprintf("HTTP %d — token rejected", resp.StatusCode),
			Latency:   latency,
			CheckedAt: now,
		}
	default:
		return Result{
			Status:    StatusUnreachable,
			Detail:    fmt.Sprintf("unexpected HTTP %d", resp.StatusCode),
			Latency:   latency,
			CheckedAt: now,
		}
	}
}

// transportDetail produces a concise, token-free message for a transport-level
// failure, distinguishing the common timeout/cancellation cases.
func transportDetail(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout: no response from Clio"
	case errors.Is(err, context.Canceled):
		return "check canceled"
	default:
		return "connection failed: " + err.Error()
	}
}
