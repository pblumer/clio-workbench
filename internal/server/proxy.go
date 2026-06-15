package server

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/pblumer/clio-workbench/internal/clio"
)

// newProxy builds the reverse proxy for /api/* against the upstream Clio.
//
// The target (URL + token) is read from the client on every request, so it
// follows the server the user picks in the GUI at runtime. Per
// docs/WORKBENCH.md §3.1/§4 the bearer token is injected server-side and never
// reaches the browser (§2 principle 4). FlushInterval is -1 so NDJSON / SSE
// streams (used by the Gegenprobe) are not buffered (§3.1).
//
// When no Clio is selected the request gets a 503 (offline mode): drafting
// still works, only push and the Gegenprobe need an instance (§3.3).
func newProxy(c *clio.Client) http.Handler {
	rp := &httputil.ReverseProxy{
		FlushInterval: -1,
		Director: func(req *http.Request) {
			base, token := c.Snapshot()
			target, err := url.Parse(base)
			if err != nil {
				return // guarded by Configured() in the wrapper below
			}
			// Strip the /api prefix the Workbench uses to namespace the proxy
			// and forward the remainder to the upstream Clio.
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.Host = target.Host
			req.URL.Path = singleJoin(target.Path, strings.TrimPrefix(req.URL.Path, "/api"))

			// Inject the token server-side; drop any client-supplied auth.
			if token != "" {
				req.Header.Set("Authorization", "Bearer "+token)
			} else {
				req.Header.Del("Authorization")
			}
		},
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !c.Configured() {
			http.Error(w,
				"no Clio selected — pick a server in the UI (or set CLIO_URL) to enable /api proxying",
				http.StatusServiceUnavailable)
			return
		}
		rp.ServeHTTP(w, r)
	})
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
