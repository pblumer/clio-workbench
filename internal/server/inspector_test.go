package server

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPrettyJSONDecodesUnicodeAndKeepsOrder(t *testing.T) {
	// lastName arrives with a ü escape (as seen from Clio).
	in := json.RawMessage(`{"firstName":"Anna","lastName":"M\u00fcller","count":3,"ok":true,"tags":["x","y"]}`)
	out := string(prettyJSON(in))

	if !strings.Contains(out, "Müller") {
		t.Errorf("expected decoded umlaut 'Müller', got:\n%s", out)
	}
	if strings.Contains(out, "\\u00fc") {
		t.Errorf("escape \\u00fc was not decoded:\n%s", out)
	}
	// field order must be preserved (not alphabetised)
	iFirst := strings.Index(out, "firstName")
	iLast := strings.Index(out, "lastName")
	iCount := strings.Index(out, "count")
	if !(iFirst < iLast && iLast < iCount) {
		t.Errorf("field order not preserved:\n%s", out)
	}
	// still valid JSON after re-encoding
	var v map[string]any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Errorf("output is not valid JSON: %v\n%s", err, out)
	}
}

func TestPrettyJSONLinksReferences(t *testing.T) {
	in := json.RawMessage(`{"employeeId":"E-000005","username":"pextern","note":"<b>x</b>"}`)
	out := string(prettyJSON(in))

	// The fk value is wrapped in a clickable ref carrying the target subject.
	if !strings.Contains(out, `<a class="ev-ref" tabindex="0" data-subject="/employees/E-000005">`) {
		t.Errorf("employeeId not linked to its subject:\n%s", out)
	}
	// A plain field stays plain text — no stray link.
	if strings.Contains(out, `data-subject="/usernames/pextern"`) {
		t.Errorf("username should not be linked:\n%s", out)
	}
	// Untrusted payload text is HTML-escaped, not rendered as markup.
	if strings.Contains(out, "<b>x</b>") {
		t.Errorf("payload markup was not escaped:\n%s", out)
	}
	if !strings.Contains(out, "&lt;b&gt;x&lt;/b&gt;") {
		t.Errorf("expected escaped markup, got:\n%s", out)
	}
}

func TestPrettyJSONEmpty(t *testing.T) {
	if got := prettyJSON(json.RawMessage(`null`)); got != "—" {
		t.Errorf("null → %q, want —", got)
	}
	if got := prettyJSON(nil); got != "—" {
		t.Errorf("nil → %q, want —", got)
	}
}
