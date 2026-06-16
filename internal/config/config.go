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
	// EventCap bounds how many events the analysis panels read from Clio.
	EventCap int
}

// Defaults mirror docs/WORKBENCH.md §3.3.
const (
	defaultAddr     = ":8080"
	defaultDataDir  = "./workbench-data"
	defaultEventCap = 50000
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
		out = append(out, strings.TrimRight(f, "/"))
	}
	if len(out) == 0 {
		return defaultServers
	}
	return out
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
