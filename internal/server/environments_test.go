package server

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestHandleEnvironmentsEmpty(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	rec := s.do(http.MethodGet, "/environments", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "environments-slot") {
		t.Errorf("missing environments-slot")
	}
}

func TestHandleSaveAndActivateEnvironment(t *testing.T) {
	s := newTestServer(t, defaultCfg())

	rec := s.do(http.MethodPost, "/environments", form(map[string]string{
		"name":       "Prod",
		"serverUrl":  "https://clio.example/",
		"subject":    "/orders",
		"types":      "created, shipped",
		"lowerBound": "0001",
		"upperBound": "9999",
		"limit":      "500",
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("save status = %d", rec.Code)
	}
	env, ok := s.envs.Active()
	if ok {
		t.Fatalf("env should not be active until activated; active=%+v", env)
	}
	list := s.envs.List()
	if len(list) != 1 || list[0].ID != "prod" {
		t.Fatalf("unexpected env list: %+v", list)
	}
	if !reflect.DeepEqual(list[0].Types, []string{"created", "shipped"}) {
		t.Errorf("types = %v", list[0].Types)
	}
	if list[0].Limit != 500 {
		t.Errorf("limit = %d", list[0].Limit)
	}

	// Activate it — sets the active env and (since it names a server) points clio.
	rec = s.do(http.MethodPost, "/environments/activate", form(map[string]string{"id": "prod"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("activate status = %d", rec.Code)
	}
	if rec.Header().Get("HX-Trigger") != "clio-changed" {
		t.Errorf("missing HX-Trigger")
	}
	if _, ok := s.envs.Active(); !ok {
		t.Errorf("env not active after activate")
	}
	if s.clio.BaseURL() != "https://clio.example" {
		t.Errorf("clio not pointed at env server: %q", s.clio.BaseURL())
	}
	// effectiveLimit now reflects the active env's 500.
	if got := s.effectiveLimit(); got != 500 {
		t.Errorf("effectiveLimit = %d, want 500", got)
	}
}

func TestHandleSaveEnvironmentValidation(t *testing.T) {
	s := newTestServer(t, defaultCfg())

	// Bad form.
	if rec := s.do(http.MethodPost, "/environments", &badReader{}); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad form status = %d", rec.Code)
	}
	// Missing name.
	if rec := s.do(http.MethodPost, "/environments", form(map[string]string{"name": ""})); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing name status = %d", rec.Code)
	}
	// Name that slugifies to empty → "invalid name" 400.
	if rec := s.do(http.MethodPost, "/environments", form(map[string]string{"name": "###"})); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid name status = %d", rec.Code)
	}
}

func TestHandleActivateEnvironmentErrors(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	// Bad form.
	if rec := s.do(http.MethodPost, "/environments/activate", &badReader{}); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad form status = %d", rec.Code)
	}
	// Unknown environment.
	if rec := s.do(http.MethodPost, "/environments/activate", form(map[string]string{"id": "ghost"})); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown env status = %d", rec.Code)
	}
}

func TestHandleDeleteEnvironment(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	s.do(http.MethodPost, "/environments", form(map[string]string{"name": "Temp"}))

	rec := s.do(http.MethodPost, "/environments/delete", form(map[string]string{"id": "temp"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d", rec.Code)
	}
	if len(s.envs.List()) != 0 {
		t.Errorf("env not deleted")
	}

	// Bad form.
	if rec := s.do(http.MethodPost, "/environments/delete", &badReader{}); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad form status = %d", rec.Code)
	}
	// Unknown environment.
	if rec := s.do(http.MethodPost, "/environments/delete", form(map[string]string{"id": "ghost"})); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown env status = %d", rec.Code)
	}
}

// TestActiveScopeWithEnv covers activeScope reading from the active environment,
// including the per-env Limit override.
func TestActiveScopeWithEnv(t *testing.T) {
	s := newTestServer(t, defaultCfg())

	// No active env → falls back to the global cap, no scope filters.
	sc := s.activeScope()
	if sc.Limit != s.cfg.EventCap || sc.Subject != "" {
		t.Fatalf("default scope = %+v", sc)
	}

	s.do(http.MethodPost, "/environments", form(map[string]string{
		"name": "Scoped", "subject": "/orders", "types": "created",
		"lowerBound": "a", "upperBound": "z", "limit": "7",
	}))
	s.do(http.MethodPost, "/environments/activate", form(map[string]string{"id": "scoped"}))

	sc = s.activeScope()
	if sc.Subject != "/orders" || sc.Limit != 7 || sc.LowerBound != "a" || sc.UpperBound != "z" {
		t.Fatalf("scoped scope = %+v", sc)
	}
	if !reflect.DeepEqual(sc.Types, []string{"created"}) {
		t.Errorf("scope types = %v", sc.Types)
	}
}

func TestSplitTypes(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"  ", nil},
		{"a,b c\nd\te", []string{"a", "b", "c", "d", "e"}},
		{"single", []string{"single"}},
	}
	for _, c := range cases {
		if got := splitTypes(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("splitTypes(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
