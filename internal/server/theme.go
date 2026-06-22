package server

import "net/http"

// theme.go holds the server side of the theme switch (docs/THEMES.md). The
// chosen palette lives in a client-written cookie; the server only *reads and
// validates* it, then renders <html data-theme="…"> so the page never flashes
// the wrong colours (FOUC-free). The cookie is not a secret — only the Clio
// token must stay server-side (principle 4) — so the write happens in theme.js.

// themeCookie is the cookie that persists the selected theme.
const themeCookie = "wb-theme"

// defaultTheme is the space look (Nebula). It renders whenever the cookie is
// absent or holds an unknown value, so an old or hand-edited cookie can never
// break the page.
const defaultTheme = "nebula"

// themeOption is one selectable palette. ID matches a [data-theme="ID"] block
// in web/static/css/themes.css; Label is what the switcher shows.
type themeOption struct {
	ID    string
	Label string
}

// themeOptions is the catalogue offered in the switcher, in display order.
// Adding a theme is: append here + add the matching block in themes.css.
var themeOptions = []themeOption{
	{ID: "nebula", Label: "Nebula"},
	{ID: "aurora", Label: "Aurora"},
	{ID: "carbon", Label: "Carbon"},
	{ID: "swiss", Label: "Swiss"},
}

// knownTheme reports whether id is a registered theme.
func knownTheme(id string) bool {
	for _, t := range themeOptions {
		if t.ID == id {
			return true
		}
	}
	return false
}

// themeFromRequest returns the validated theme id for this request, falling
// back to the default when the cookie is missing or unrecognised.
func themeFromRequest(r *http.Request) string {
	c, err := r.Cookie(themeCookie)
	if err != nil {
		return defaultTheme
	}
	if knownTheme(c.Value) {
		return c.Value
	}
	return defaultTheme
}
