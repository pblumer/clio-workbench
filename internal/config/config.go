// Package config reads the Workbench configuration from the environment.
//
// The variables follow the Clio style (see docs/WORKBENCH.md §3.3). Without
// CLIO_URL/token the Workbench works purely offline on the draft; only push
// and the conformance check (Gegenprobe) need a running instance.
package config

import (
	"os"
	"strconv"
	"strings"
)

// Config holds the runtime configuration of the Workbench.
type Config struct {
	// Addr is the listen address, e.g. ":8080".
	Addr string
	// DataDir is where drafts are persisted.
	DataDir string
	// ClioURL is the upstream Clio base URL used for push and the Gegenprobe.
	// Empty means the Workbench runs offline (proxy disabled).
	ClioURL string
	// ClioToken is the bearer token injected server-side into proxied
	// requests. It is never exposed to the browser.
	ClioToken string
	// Servers are preset Clio URLs offered for quick selection in the UI.
	Servers []string
	// EventCap optionally bounds how many events the analysis panels read from
	// Clio. The default is 0 — no cap, every event is loaded. A positive
	// WORKBENCH_EVENT_CAP (or a per-environment limit) opts into a read limit
	// to protect against over-reads on very large stores.
	EventCap int
	// SpaceMaxRows, SpaceMaxDots and SpaceCols tune the Event Space's
	// level-of-detail switch (docs/SPACE-LOD.md): the subject-row budget, the
	// charted-event budget beyond which the view aggregates into a density grid,
	// and that grid's time-column count. Each defaults to 0 — the built-in value
	// is used; a positive override opts into a different budget.
	SpaceMaxRows int
	SpaceMaxDots int
	SpaceCols    int
}

// Defaults mirror docs/WORKBENCH.md §3.3.
const (
	defaultAddr    = ":8080"
	defaultDataDir = "./workbench-data"
	// defaultEventCap is 0: no read cap by default. The cap is opt-in via a
	// positive WORKBENCH_EVENT_CAP or an environment limit.
	defaultEventCap = 0
)

// defaultServers are offered in the connect menu when WORKBENCH_SERVERS is unset.
var defaultServers = []string{"https://clio.blumer.cloud"}

// Load reads the configuration from the environment, applying defaults.
func Load() Config {
	return Config{
		Addr:      envOr("WORKBENCH_ADDR", defaultAddr),
		DataDir:   envOr("WORKBENCH_DATA", defaultDataDir),
		ClioURL:   strings.TrimRight(os.Getenv("CLIO_URL"), "/"),
		ClioToken: os.Getenv("CLIO_API_TOKEN"),
		Servers:   serverList(os.Getenv("WORKBENCH_SERVERS")),
		EventCap:  intEnvOr("WORKBENCH_EVENT_CAP", defaultEventCap),
		// 0 → the Event Space uses its built-in level-of-detail budgets.
		SpaceMaxRows: intEnvOr("WORKBENCH_SPACE_MAX_ROWS", 0),
		SpaceMaxDots: intEnvOr("WORKBENCH_SPACE_MAX_DOTS", 0),
		SpaceCols:    intEnvOr("WORKBENCH_SPACE_COLS", 0),
	}
}

func intEnvOr(key string, fallback int) int {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return fallback
}

// serverList parses a comma/space/newline-separated list of preset Clio URLs,
// falling back to the built-in defaults when empty.
func serverList(s string) []string {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\n' || r == '\t' || r == '\r'
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		out = append(out, normalizeBaseURL(f))
	}
	if len(out) == 0 {
		return defaultServers
	}
	return out
}

// normalizeBaseURL trimmt Slashes und entfernt ein redundantes "/api/v1"-Suffix
// aus einer Preset-URL. Der clio.Client hängt "/api/v1/..." selbst an; eine
// durchgeschleuste API-URL würde den Pfad sonst verdoppeln (→ 404/UNREACHABLE).
// Die Logik spiegelt clio.normalizeBaseURL — hier lokal gehalten, um einen
// Import-Zyklus zwischen config und clio zu vermeiden.
func normalizeBaseURL(raw string) string {
	u := strings.TrimRight(strings.TrimSpace(raw), "/")
	u = strings.TrimSuffix(u, "/api/v1")
	return strings.TrimRight(u, "/")
}

// ProxyEnabled reports whether an upstream Clio is configured, which is the
// precondition for the /api/* reverse proxy.
func (c Config) ProxyEnabled() bool {
	return c.ClioURL != ""
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
