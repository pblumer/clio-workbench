package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/pblumer/clio-workbench/internal/clio"
)

// connectionTimeout bounds a /connection probe so a slow Clio cannot hang the
// request handler.
const connectionTimeout = 6 * time.Second

// connectionView is the view model for the connection-status fragment. It is
// deliberately token-free: only the status, a label, a safe detail message,
// the upstream URL and the measured latency reach the browser.
type connectionView struct {
	Status      clio.Status
	Label       string
	Detail      string
	ClioURL     string
	LatencyMS   int64
	HasInfo     bool
	EventsTotal int64
}

// statusLabels maps a status onto the HUD label shown in the header pill.
var statusLabels = map[clio.Status]string{
	clio.StatusOnline:       "UPLINK",
	clio.StatusUnauthorized: "AUTH FAIL",
	clio.StatusUnreachable:  "UNREACHABLE",
	clio.StatusOffline:      "OFFLINE",
}

// handleConnection probes the upstream Clio and renders the live status
// fragment (HTMX). The header loads it on page load and the reconnect button
// re-requests it.
func (s *Server) handleConnection(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), connectionTimeout)
	defer cancel()

	s.render(w, "connection.html", s.connectionView(ctx, s.clio.CheckConnection(ctx)))
}

// connectionView builds the view; when online it also fetches the event count
// (Clio /api/v1/info) so the header shows how much data the instance holds.
func (s *Server) connectionView(ctx context.Context, res clio.Result) connectionView {
	v := connectionView{
		Status:    res.Status,
		Label:     statusLabels[res.Status],
		Detail:    res.Detail,
		ClioURL:   s.clio.BaseURL(),
		LatencyMS: res.Latency.Milliseconds(),
	}
	if res.Status == clio.StatusOnline {
		if info, err := s.clio.FetchInfo(ctx); err == nil {
			v.HasInfo = true
			v.EventsTotal = info.EventsTotal
		}
	}
	return v
}

// handleConnect points the Workbench at the Clio server chosen in the GUI,
// then probes it and returns the updated status fragment. The token is taken
// from the form and held server-side only — it is never echoed back.
func (s *Server) handleConnect(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	url := strings.TrimSpace(r.FormValue("url"))
	token := r.FormValue("token")
	s.clio.SetTarget(url, token)
	s.log.Info("clio target set", "url", s.clio.BaseURL(), "token", s.clio.HasToken())

	// Tell the events panel (and anything else listening) to refresh.
	w.Header().Set("HX-Trigger", "clio-changed")

	ctx, cancel := context.WithTimeout(r.Context(), connectionTimeout)
	defer cancel()
	s.render(w, "connection.html", s.connectionView(ctx, s.clio.CheckConnection(ctx)))
}

// handleDisconnect clears the selected Clio (back to offline) and returns the
// updated status fragment.
func (s *Server) handleDisconnect(w http.ResponseWriter, r *http.Request) {
	s.clio.SetTarget("", "")
	s.log.Info("clio target cleared")
	w.Header().Set("HX-Trigger", "clio-changed")

	ctx, cancel := context.WithTimeout(r.Context(), connectionTimeout)
	defer cancel()
	s.render(w, "connection.html", s.connectionView(ctx, s.clio.CheckConnection(ctx)))
}

// LogConnectionCheck runs one connection probe and logs the outcome. It is
// meant to be called non-blocking at startup: it never fails the server, so
// offline drafting stays possible even when Clio is down (docs/WORKBENCH.md
// §3.3).
func (s *Server) LogConnectionCheck(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, connectionTimeout)
	defer cancel()

	res := s.clio.CheckConnection(ctx)
	attrs := []any{
		"status", string(res.Status),
		"detail", res.Detail,
		"latency_ms", res.Latency.Milliseconds(),
	}
	switch res.Status {
	case clio.StatusOnline, clio.StatusOffline:
		s.log.Info("clio connection check", attrs...)
	default:
		s.log.Warn("clio connection check", attrs...)
	}
}
