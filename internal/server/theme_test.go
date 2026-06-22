package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestThemeFromRequest(t *testing.T) {
	tests := []struct {
		name   string
		cookie string // "" means no cookie set
		want   string
	}{
		{"no cookie falls back to default", "", defaultTheme},
		{"valid theme is honoured", "aurora", "aurora"},
		{"another valid theme", "swiss", "swiss"},
		{"unknown theme falls back", "midnight", defaultTheme},
		{"empty value falls back", "  ", defaultTheme},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.cookie != "" {
				req.AddCookie(&http.Cookie{Name: themeCookie, Value: strings.TrimSpace(tc.cookie)})
			}
			if got := themeFromRequest(req); got != tc.want {
				t.Fatalf("themeFromRequest = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestKnownTheme(t *testing.T) {
	for _, id := range []string{"nebula", "aurora", "carbon", "swiss"} {
		if !knownTheme(id) {
			t.Errorf("knownTheme(%q) = false, want true", id)
		}
	}
	for _, id := range []string{"", "Nebula", "space", "light"} {
		if knownTheme(id) {
			t.Errorf("knownTheme(%q) = true, want false", id)
		}
	}
}

// doWithCookie issues a GET carrying a theme cookie and returns the response.
func doWithCookie(s *Server, target, theme string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if theme != "" {
		req.AddCookie(&http.Cookie{Name: themeCookie, Value: theme})
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func TestHandleIndexRendersTheme(t *testing.T) {
	s := newTestServer(t, defaultCfg())

	// Default: no cookie → the space look, with the theme stylesheet linked
	// before the structural one and every theme offered in the switcher.
	rec := doWithCookie(s, "/", "")
	body := rec.Body.String()
	if !strings.Contains(body, `data-theme="nebula"`) {
		t.Errorf("default index missing nebula data-theme")
	}
	if !strings.Contains(body, `/static/css/themes.css`) {
		t.Errorf("index does not link themes.css")
	}
	if i, j := strings.Index(body, "themes.css"), strings.Index(body, "workbench.css"); i < 0 || j < 0 || i > j {
		t.Errorf("themes.css must be linked before workbench.css (i=%d j=%d)", i, j)
	}
	for _, label := range []string{"Nebula", "Aurora", "Carbon", "Swiss"} {
		if !strings.Contains(body, ">"+label+"<") {
			t.Errorf("theme switcher missing option %q", label)
		}
	}

	// A valid cookie selects that palette (FOUC-free server render).
	if b := doWithCookie(s, "/", "aurora").Body.String(); !strings.Contains(b, `data-theme="aurora"`) {
		t.Errorf("index did not honour aurora cookie")
	}
	// A bogus cookie never breaks the page.
	if b := doWithCookie(s, "/", "bogus").Body.String(); !strings.Contains(b, `data-theme="nebula"`) {
		t.Errorf("index did not fall back for unknown theme")
	}
}

func TestHandleEditorRendersTheme(t *testing.T) {
	s := newTestServer(t, defaultCfg())
	id := seedDraft(t, s, "Themed")

	rec := doWithCookie(s, "/editor/"+id, "carbon")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("editor status = %d", rec.Code)
	}
	if !strings.Contains(body, `data-theme="carbon"`) {
		t.Errorf("editor did not honour carbon cookie")
	}
	// The embedded draft's fields must still render through promotion.
	if !strings.Contains(body, "Themed") {
		t.Errorf("editor page lost the draft name after wrapping")
	}
	if !strings.Contains(body, "/static/css/themes.css") {
		t.Errorf("editor does not link themes.css")
	}
}
