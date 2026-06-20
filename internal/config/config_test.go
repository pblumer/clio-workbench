package config

import (
	"reflect"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	// Ensure all relevant env vars are unset/empty.
	t.Setenv("WORKBENCH_ADDR", "")
	t.Setenv("WORKBENCH_DATA", "")
	t.Setenv("CLIO_URL", "")
	t.Setenv("CLIO_API_TOKEN", "")
	t.Setenv("WORKBENCH_SERVERS", "")
	t.Setenv("WORKBENCH_EVENT_CAP", "")

	c := Load()
	if c.Addr != defaultAddr {
		t.Errorf("Addr = %q, want %q", c.Addr, defaultAddr)
	}
	if c.DataDir != defaultDataDir {
		t.Errorf("DataDir = %q, want %q", c.DataDir, defaultDataDir)
	}
	if c.ClioURL != "" {
		t.Errorf("ClioURL = %q, want empty", c.ClioURL)
	}
	if c.ClioToken != "" {
		t.Errorf("ClioToken = %q, want empty", c.ClioToken)
	}
	if !reflect.DeepEqual(c.Servers, defaultServers) {
		t.Errorf("Servers = %v, want %v", c.Servers, defaultServers)
	}
	if c.EventCap != defaultEventCap {
		t.Errorf("EventCap = %d, want %d", c.EventCap, defaultEventCap)
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("WORKBENCH_ADDR", ":9090")
	t.Setenv("WORKBENCH_DATA", "/tmp/data")
	t.Setenv("CLIO_URL", "https://clio.example.com/")
	t.Setenv("CLIO_API_TOKEN", "secret-token")
	t.Setenv("WORKBENCH_SERVERS", "https://a.example.com/, https://b.example.com")
	t.Setenv("WORKBENCH_EVENT_CAP", "100")

	c := Load()
	if c.Addr != ":9090" {
		t.Errorf("Addr = %q", c.Addr)
	}
	if c.DataDir != "/tmp/data" {
		t.Errorf("DataDir = %q", c.DataDir)
	}
	// Trailing slash must be trimmed.
	if c.ClioURL != "https://clio.example.com" {
		t.Errorf("ClioURL = %q", c.ClioURL)
	}
	if c.ClioToken != "secret-token" {
		t.Errorf("ClioToken = %q", c.ClioToken)
	}
	want := []string{"https://a.example.com", "https://b.example.com"}
	if !reflect.DeepEqual(c.Servers, want) {
		t.Errorf("Servers = %v, want %v", c.Servers, want)
	}
	if c.EventCap != 100 {
		t.Errorf("EventCap = %d, want 100", c.EventCap)
	}
}

func TestIntEnvOr(t *testing.T) {
	const key = "WORKBENCH_EVENT_CAP"
	tests := []struct {
		name string
		val  string
		want int
	}{
		{"empty falls back", "", defaultEventCap},
		{"whitespace falls back", "   ", defaultEventCap},
		{"non-numeric falls back", "abc", defaultEventCap},
		{"zero falls back", "0", defaultEventCap},
		{"negative falls back", "-5", defaultEventCap},
		{"valid positive", "42", 42},
		{"trimmed valid", "  7  ", 7},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(key, tt.val)
			if got := intEnvOr(key, defaultEventCap); got != tt.want {
				t.Errorf("intEnvOr(%q) = %d, want %d", tt.val, got, tt.want)
			}
		})
	}
}

func TestServerList(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"empty uses defaults", "", defaultServers},
		{"whitespace only uses defaults", " \n\t ", defaultServers},
		{"comma separated", "a,b", []string{"a", "b"}},
		{"space separated", "a b", []string{"a", "b"}},
		{"newline separated", "a\nb", []string{"a", "b"}},
		{"tab and carriage return", "a\tb\rc", []string{"a", "b", "c"}},
		{"mixed with trailing slashes", "https://a/ , https://b/", []string{"https://a", "https://b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := serverList(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("serverList(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestEnvOr(t *testing.T) {
	const key = "WORKBENCH_ADDR"
	t.Run("empty falls back", func(t *testing.T) {
		t.Setenv(key, "")
		if got := envOr(key, "fb"); got != "fb" {
			t.Errorf("got %q, want fb", got)
		}
	})
	t.Run("whitespace falls back", func(t *testing.T) {
		t.Setenv(key, "   ")
		if got := envOr(key, "fb"); got != "fb" {
			t.Errorf("got %q, want fb", got)
		}
	})
	t.Run("set value trimmed", func(t *testing.T) {
		t.Setenv(key, "  :1234  ")
		if got := envOr(key, "fb"); got != ":1234" {
			t.Errorf("got %q, want :1234", got)
		}
	})
}

func TestProxyEnabled(t *testing.T) {
	if (Config{ClioURL: ""}).ProxyEnabled() {
		t.Error("empty ClioURL should disable proxy")
	}
	if !(Config{ClioURL: "https://x"}).ProxyEnabled() {
		t.Error("set ClioURL should enable proxy")
	}
}
