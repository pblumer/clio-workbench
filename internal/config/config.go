// Package config reads the Workbench configuration from the environment.
//
// The variables follow the Clio style (see docs/WORKBENCH.md §3.3). Without
// CLIO_URL/token the Workbench works purely offline on the draft; only push
// and the conformance check (Gegenprobe) need a running instance.
package config

import (
	"os"
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
}

// Defaults mirror docs/WORKBENCH.md §3.3.
const (
	defaultAddr    = ":8080"
	defaultDataDir = "./workbench-data"
)

// Load reads the configuration from the environment, applying defaults.
func Load() Config {
	return Config{
		Addr:      envOr("WORKBENCH_ADDR", defaultAddr),
		DataDir:   envOr("WORKBENCH_DATA", defaultDataDir),
		ClioURL:   strings.TrimRight(os.Getenv("CLIO_URL"), "/"),
		ClioToken: os.Getenv("CLIO_API_TOKEN"),
	}
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
