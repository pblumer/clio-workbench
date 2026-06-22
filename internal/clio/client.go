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
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
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

// Client probes a Clio instance. The target (base URL + token) can be changed
// at runtime — e.g. when the user picks a server in the GUI — so it is guarded
// by a mutex. It is safe for concurrent use.
type Client struct {
	mu      sync.RWMutex
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

// normalizeBaseURL trimmt Whitespace/Slashes und entfernt ein redundantes
// "/api/v1"-Suffix, da der Client "/api/v1/..." selbst anhängt. So heilt sich
// die Basis-URL selbst, wenn jemand die volle API-URL einträgt (z. B. aus
// CLIO_URL="…/api/v1") — sonst verdoppelt sich der Pfad zu "…/api/v1/api/v1/…"
// und Clio antwortet mit 404 (sichtbar als UNREACHABLE).
func normalizeBaseURL(raw string) string {
	u := strings.TrimRight(strings.TrimSpace(raw), "/")
	u = strings.TrimSuffix(u, "/api/v1")
	return strings.TrimRight(u, "/")
}

// New builds a Client for the given Clio base URL and bearer token. An empty
// baseURL yields a client that always reports StatusOffline.
func New(baseURL, token string, opts ...Option) *Client {
	c := &Client{
		baseURL: normalizeBaseURL(baseURL),
		token:   token,
		httpc:   &http.Client{Timeout: defaultTimeout},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// SetTarget changes the upstream Clio the client talks to. An empty baseURL
// clears the target (back to offline). The token is held server-side only.
func (c *Client) SetTarget(baseURL, token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.baseURL = normalizeBaseURL(baseURL)
	c.token = token
}

// Configured reports whether an upstream Clio URL is set.
func (c *Client) Configured() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.baseURL != ""
}

// BaseURL returns the currently configured upstream URL (safe to show in the
// UI). The token is intentionally not exposed here.
func (c *Client) BaseURL() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.baseURL
}

// HasToken reports whether a token is set, without revealing it.
func (c *Client) HasToken() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.token != ""
}

// Snapshot returns the current target (base URL + token) atomically. It is
// used by the reverse proxy within the same process; the token never leaves
// the server.
func (c *Client) Snapshot() (baseURL, token string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.baseURL, c.token
}

// CheckConnection probes Clio and maps the outcome onto a Status. It never
// returns an error: every failure mode is expressed as a Result so callers can
// render it directly. Honour the caller's context for cancellation/deadline.
func (c *Client) CheckConnection(ctx context.Context) Result {
	now := time.Now()
	base, token := c.Snapshot()
	if base == "" {
		return Result{
			Status:    StatusOffline,
			Detail:    "no Clio selected — drafting works offline",
			CheckedAt: now,
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+probePath, nil)
	if err != nil {
		return Result{Status: StatusUnreachable, Detail: err.Error(), CheckedAt: now}
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
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

// Sentinel errors returned by read operations so callers can map them onto the
// same status vocabulary as CheckConnection.
var (
	// ErrOffline means no CLIO_URL is configured.
	ErrOffline = errors.New("clio: no CLIO_URL configured")
	// ErrUnauthorized means Clio rejected the bearer token (HTTP 401/403).
	ErrUnauthorized = errors.New("clio: token rejected")
)

// EventType is one entry of read-event-types: an event type with how often it
// has occurred and whether a schema is registered. Matches Clio's NDJSON line
// shape {"type":..,"count":..,"hasSchema":..}.
type EventType struct {
	Type      string `json:"type"`
	Count     int    `json:"count"`
	HasSchema bool   `json:"hasSchema"`
}

// ReadEventTypes lists the event types written to Clio so far (with counts), by
// consuming the NDJSON stream of GET /api/v1/read-event-types. The token is
// injected server-side. Returns ErrOffline / ErrUnauthorized for those states.
func (c *Client) ReadEventTypes(ctx context.Context) ([]EventType, error) {
	base, token := c.Snapshot()
	if base == "" {
		return nil, ErrOffline
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+probePath, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/x-ndjson")

	resp, err := c.httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		// fall through to parse
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, ErrUnauthorized
	default:
		return nil, fmt.Errorf("clio: unexpected HTTP %d", resp.StatusCode)
	}

	var types []EventType
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var et EventType
		if err := json.Unmarshal([]byte(line), &et); err != nil {
			return nil, fmt.Errorf("clio: decode event type: %w", err)
		}
		types = append(types, et)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("clio: read event types: %w", err)
	}
	return types, nil
}

// Event is a minimal projection of a Clio event: enough to reconstruct the
// per-subject type sequences for process discovery. Clio returns events in
// append (monotone-id) order, so the stream order is the chronological order.
type Event struct {
	ID      string `json:"id"`
	Source  string `json:"source"`
	Subject string `json:"subject"`
	Type    string `json:"type"`
	Time    string `json:"time"`
}

// Info is a subset of Clio's GET /api/v1/info (server metadata/telemetry).
type Info struct {
	EventsTotal       int64  `json:"eventsTotal"`
	ActiveObservers   int64  `json:"activeObservers"`
	DatabaseFileBytes int64  `json:"databaseFileBytes"`
	Uptime            string `json:"uptime"`
}

// FetchInfo reads GET /api/v1/info so the UI can show how many events the
// connected instance actually has (a quick "is this the right/current store?"
// check). Token injected server-side.
func (c *Client) FetchInfo(ctx context.Context) (*Info, error) {
	base, token := c.Snapshot()
	if base == "" {
		return nil, ErrOffline
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v1/info", nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, ErrUnauthorized
	default:
		return nil, fmt.Errorf("clio: unexpected HTTP %d", resp.StatusCode)
	}
	var info Info
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&info); err != nil {
		return nil, fmt.Errorf("clio: decode info: %w", err)
	}
	return &info, nil
}

// eventsPath is Clio's convenient root read route (all events as NDJSON).
const eventsPath = "/api/v1/events"

// Scope narrows an event read to a subject subtree, type set and/or id range.
type Scope struct {
	Subject    string
	Types      []string
	LowerBound string
	UpperBound string
	Limit      int
}

func (c *Client) scopedURL(base string, sc Scope) string {
	path := eventsPath
	if s := strings.Trim(sc.Subject, "/"); s != "" {
		path += "/" + s
	}
	q := url.Values{}
	q.Set("recursive", "true")
	for _, t := range sc.Types {
		if t != "" {
			q.Add("type", t)
		}
	}
	if sc.LowerBound != "" {
		q.Set("lowerBound", sc.LowerBound)
	}
	if sc.UpperBound != "" {
		q.Set("upperBound", sc.UpperBound)
	}
	return base + path + "?" + q.Encode()
}

// ReadScoped streams the minimal event projection for a scope.
func (c *Client) ReadScoped(ctx context.Context, sc Scope) ([]Event, error) {
	base, _ := c.Snapshot()
	if base == "" {
		return nil, ErrOffline
	}
	return c.readEventsURL(ctx, c.scopedURL(base, sc), sc.Limit)
}

// ReadFullScoped streams events with their data payload for a scope.
func (c *Client) ReadFullScoped(ctx context.Context, sc Scope) ([]FullEvent, error) {
	base, _ := c.Snapshot()
	if base == "" {
		return nil, ErrOffline
	}
	return c.readFullEventsURL(ctx, c.scopedURL(base, sc), sc.Limit)
}

// readFullEventsURL performs the shared NDJSON read for the FullEvent shape.
func (c *Client) readFullEventsURL(ctx context.Context, fullURL string, limit int) ([]FullEvent, error) {
	base, token := c.Snapshot()
	if base == "" {
		return nil, ErrOffline
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/x-ndjson")

	resp, err := c.httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, ErrUnauthorized
	default:
		return nil, fmt.Errorf("clio: unexpected HTTP %d", resp.StatusCode)
	}

	var events []FullEvent
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var ev FullEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			return nil, fmt.Errorf("clio: decode event: %w", err)
		}
		events = append(events, ev)
		if limit > 0 && len(events) >= limit {
			break
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("clio: read events: %w", err)
	}
	return events, nil
}

// ReadEvents streams up to limit events from Clio (root, recursive) as NDJSON.
// A limit <= 0 reads all available events. The token is injected server-side;
// ErrOffline / ErrUnauthorized are returned for those states.
func (c *Client) ReadEvents(ctx context.Context, limit int) ([]Event, error) {
	return c.readEventsURL(ctx, c.eventsURL(eventsPath+"?recursive=true"), limit)
}

// ReadEventsUnder streams up to limit events under a subject prefix via
// GET /api/v1/events/<prefix>?recursive=true — much cheaper than reading the
// whole store when a conformance check is scoped to a collection.
func (c *Client) ReadEventsUnder(ctx context.Context, subjectPrefix string, limit int) ([]Event, error) {
	p := strings.Trim(strings.TrimSpace(subjectPrefix), "/")
	if p == "" {
		return c.ReadEvents(ctx, limit)
	}
	return c.readEventsURL(ctx, c.eventsURL(eventsPath+"/"+p+"?recursive=true"), limit)
}

func (c *Client) eventsURL(path string) string {
	base, _ := c.Snapshot()
	return base + path
}

// readEventsURL performs the shared NDJSON read for the minimal Event shape.
func (c *Client) readEventsURL(ctx context.Context, fullURL string, limit int) ([]Event, error) {
	base, token := c.Snapshot()
	if base == "" {
		return nil, ErrOffline
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/x-ndjson")

	resp, err := c.httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, ErrUnauthorized
	default:
		return nil, fmt.Errorf("clio: unexpected HTTP %d", resp.StatusCode)
	}

	var events []Event
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var ev Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			return nil, fmt.Errorf("clio: decode event: %w", err)
		}
		events = append(events, ev)
		if limit > 0 && len(events) >= limit {
			break
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("clio: read events: %w", err)
	}
	return events, nil
}

// FullEvent is an event including its data payload, for the node inspector.
type FullEvent struct {
	ID      string          `json:"id"`
	Source  string          `json:"source"`
	Subject string          `json:"subject"`
	Type    string          `json:"type"`
	Time    string          `json:"time"`
	Data    json.RawMessage `json:"data"`
}

// ReadFullEvents streams up to limit events (with their data payload) via
// GET /api/v1/events?recursive=true. Token injected server-side.
func (c *Client) ReadFullEvents(ctx context.Context, limit int) ([]FullEvent, error) {
	base, token := c.Snapshot()
	if base == "" {
		return nil, ErrOffline
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+eventsPath+"?recursive=true", nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/x-ndjson")

	resp, err := c.httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, ErrUnauthorized
	default:
		return nil, fmt.Errorf("clio: unexpected HTTP %d", resp.StatusCode)
	}

	var events []FullEvent
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var ev FullEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			return nil, fmt.Errorf("clio: decode event: %w", err)
		}
		events = append(events, ev)
		if limit > 0 && len(events) >= limit {
			break
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("clio: read full events: %w", err)
	}
	return events, nil
}

// ReadEventsByType streams up to limit events of one type (with their data
// payload) via GET /api/v1/events?type=<type>. Token injected server-side;
// ErrOffline / ErrUnauthorized returned for those states.
func (c *Client) ReadEventsByType(ctx context.Context, typ string, limit int) ([]FullEvent, error) {
	base, token := c.Snapshot()
	if base == "" {
		return nil, ErrOffline
	}

	u := base + eventsPath + "?recursive=true&type=" + url.QueryEscape(typ)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/x-ndjson")

	resp, err := c.httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		// parse below
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, ErrUnauthorized
	default:
		return nil, fmt.Errorf("clio: unexpected HTTP %d", resp.StatusCode)
	}

	var events []FullEvent
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var ev FullEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			return nil, fmt.Errorf("clio: decode event: %w", err)
		}
		events = append(events, ev)
		if limit > 0 && len(events) >= limit {
			break
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("clio: read events by type: %w", err)
	}
	return events, nil
}

// appendPath is Clio's public event-append endpoint (structured CloudEvents).
const appendPath = "/api/v1/events"

// AppendEvent posts one CloudEvent (structured mode) to Clio. envelope is the
// complete CloudEvents JSON object. The token is injected server-side.
// ErrOffline / ErrUnauthorized are returned for those states; any other non-2xx
// becomes an error so callers can surface a failed write.
func (c *Client) AppendEvent(ctx context.Context, envelope []byte) error {
	base, token := c.Snapshot()
	if base == "" {
		return ErrOffline
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+appendPath, bytes.NewReader(envelope))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/cloudevents+json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxDrain))
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return ErrUnauthorized
	default:
		return fmt.Errorf("clio: append returned HTTP %d", resp.StatusCode)
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
