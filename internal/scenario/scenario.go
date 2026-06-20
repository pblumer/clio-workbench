// Package scenario holds the Test Studio's test artifacts — suites of named
// scenarios checked against a designed model — and persists them as local JSON
// files (docs/TESTSTUDIO.md §5, roadmap WP-3).
//
// Like internal/store, suites are git-friendly local files. They live one per
// file under <DataDir>/scenarios/ — a *subdirectory*, deliberately: the draft
// store lists every <DataDir>/*.json and decodes it as a Draft, so suites must
// not share that namespace. The store is safe for concurrent use.
//
// A suite references a draft (DraftID) and records the model revision it was
// written against (DraftRev) so the studio can warn when the model has drifted
// out from under its tests.
package scenario

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pblumer/clio-workbench/internal/model"
)

// Outcome is what a scenario expects the engine to say about its sequence.
type Outcome string

const (
	// ExpectAccept: the event-type sequence must be a valid walk of the model.
	ExpectAccept Outcome = "accept"
	// ExpectReject: the sequence must be rejected (a deliberate negative test).
	ExpectReject Outcome = "reject"
)

// Expectation is the assertion a case makes about its sequence.
type Expectation struct {
	Outcome Outcome `json:"outcome"`
	// EndState optionally requires the accepted walk to finish in this node
	// (id or label). Only meaningful with ExpectAccept. Empty = don't care.
	EndState string `json:"endState,omitempty"`
}

// Step is one event in a scenario sequence: an event-type name (an edge in the
// model), optionally with an explicit subject and/or a concrete data payload.
// When Data is absent the generator may fake a schema-valid payload (WP-5).
type Step struct {
	Type    string          `json:"type"`
	Subject string          `json:"subject,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Case is a single named scenario: a sequence plus its expectation.
type Case struct {
	ID     string      `json:"id"`
	Name   string      `json:"name"`
	Steps  []Step      `json:"steps,omitempty"`
	Expect Expectation `json:"expect"`
	// Seed makes any generated parts of the case reproducible (WP-5/§4.4).
	Seed int64 `json:"seed,omitempty"`
}

// Suite is a versionable collection of scenarios for one model.
type Suite struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	DraftID  string `json:"draftId"`
	DraftRev string `json:"draftRev,omitempty"`
	Cases    []Case `json:"cases,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Validate checks structural integrity of a suite.
func (s *Suite) Validate() error {
	if !model.ValidID(s.ID) {
		return fmt.Errorf("invalid suite id %q: must be a slug", s.ID)
	}
	if strings.TrimSpace(s.Name) == "" {
		return errors.New("suite name must not be empty")
	}
	if s.DraftID == "" {
		return errors.New("suite must reference a draft")
	}
	seen := make(map[string]struct{}, len(s.Cases))
	for _, c := range s.Cases {
		if c.ID == "" {
			return errors.New("case id must not be empty")
		}
		if _, dup := seen[c.ID]; dup {
			return fmt.Errorf("duplicate case id %q", c.ID)
		}
		seen[c.ID] = struct{}{}
		switch c.Expect.Outcome {
		case ExpectAccept, ExpectReject:
		default:
			return fmt.Errorf("invalid expectation %q in case %q", c.Expect.Outcome, c.ID)
		}
	}
	return nil
}

// DraftRev is a stable revision fingerprint of the *test-relevant* content of a
// draft: its event-type edges (transitions) and its named event steps with
// their fields. It deliberately ignores canvas layout and timestamps, so only
// changes that can actually affect a scenario's outcome move the revision.
func DraftRev(d model.Draft) string {
	type revField struct {
		Name, Type, Format, Ref string
		Required                bool
		Enum                    []string
	}
	type revEvent struct {
		Name   string
		Fields []revField
	}
	type revEdge struct{ Type, From, To string }
	type proj struct {
		Edges  []revEdge
		Events []revEvent
	}

	var p proj
	for _, e := range d.Edges {
		p.Edges = append(p.Edges, revEdge{Type: e.Type, From: e.From, To: e.To})
	}
	for _, st := range d.Steps {
		if st.Kind != model.StepEvent {
			continue
		}
		ev := revEvent{Name: st.Name}
		for _, f := range st.Fields {
			ev.Fields = append(ev.Fields, revField{
				Name: f.Name, Type: f.Type, Format: f.Format, Ref: f.Ref,
				Required: f.Required, Enum: f.Enum,
			})
		}
		p.Events = append(p.Events, ev)
	}

	b, _ := json.Marshal(p)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:12]
}

// Drift reports whether the suite was written against a different model revision
// than the draft now has. A suite with no recorded revision never drifts.
func Drift(s Suite, d model.Draft) bool {
	return s.DraftRev != "" && s.DraftRev != DraftRev(d)
}
