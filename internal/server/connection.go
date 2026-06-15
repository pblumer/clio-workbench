package server

import (
	"context"
	"net/http"
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
	Status    clio.Status
	Label     string
	Detail    string
	ClioURL   string
	LatencyMS int64
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

	res := s.clio.CheckConnection(ctx)
	s.render(w, "connection.html", s.connectionView(res))
}

func (s *Server) connectionView(res clio.Result) connectionView {
	return connectionView{
		Status:    res.Status,
		Label:     statusLabels[res.Status],
		Detail:    res.Detail,
		ClioURL:   s.cfg.ClioURL,
		LatencyMS: res.Latency.Milliseconds(),
	}
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
