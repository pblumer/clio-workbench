package server

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/pblumer/clio-workbench/internal/config"
)

// newProxy builds the reverse proxy for /api/* against the upstream Clio.
//
// Per docs/WORKBENCH.md §3.1/§4: the bearer token is injected server-side and
// never reaches the browser (§2 principle 4). FlushInterval is -1 so NDJSON /
// SSE streams (used by the Gegenprobe) are not buffered (§3.1).
func newProxy(cfg config.Config) (http.Handler, error) {
	target, err := url.Parse(cfg.ClioURL)
	if err != nil {
		return nil, fmt.Errorf("parse CLIO_URL: %w", err)
	}

	rp := &httputil.ReverseProxy{
		FlushInterval: -1,
		Director: func(req *http.Request) {
			// Strip the /api prefix the Workbench uses to namespace the proxy
			// and forward the remainder to the upstream Clio.
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.Host = target.Host
			req.URL.Path = singleJoin(target.Path, strings.TrimPrefix(req.URL.Path, "/api"))

			// Inject the token server-side; drop any client-supplied auth.
			if cfg.ClioToken != "" {
				req.Header.Set("Authorization", "Bearer "+cfg.ClioToken)
			} else {
				req.Header.Del("Authorization")
			}
		},
	}
	return http.StripPrefix("", rp), nil
}

// singleJoin joins two URL path segments with exactly one slash.
func singleJoin(a, b string) string {
	switch {
	case a == "" || a == "/":
		if b == "" {
			return "/"
		}
		return "/" + strings.TrimPrefix(b, "/")
	case b == "":
		return a
	default:
		return strings.TrimRight(a, "/") + "/" + strings.TrimPrefix(b, "/")
	}
}

// proxyDisabledHandler reports that no upstream Clio is configured. The
// Workbench still works offline (drafting); only push and the Gegenprobe need
// an instance (§3.3).
func proxyDisabledHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w,
			"no upstream Clio configured — set CLIO_URL (and CLIO_API_TOKEN) to enable /api proxying",
			http.StatusServiceUnavailable)
	})
}
