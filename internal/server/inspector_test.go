package server

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPrettyJSONDecodesUnicodeAndKeepsOrder(t *testing.T) {
	// lastName arrives with a ü escape (as seen from Clio).
	in := json.RawMessage(`{"firstName":"Anna","lastName":"M\u00fcller","count":3,"ok":true,"tags":["x","y"]}`)
	out := prettyJSON(in)

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

func TestPrettyJSONEmpty(t *testing.T) {
	if got := prettyJSON(json.RawMessage(`null`)); got != "—" {
		t.Errorf("null → %q, want —", got)
	}
	if got := prettyJSON(nil); got != "—" {
		t.Errorf("nil → %q, want —", got)
	}
}
