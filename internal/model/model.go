// Package model defines the shared draft data model of the Workbench.
//
// As described in docs/WORKBENCH.md §5.1, a draft is — regardless of the
// rendering view (state machine or BPMN) — a directed graph:
//
//   - Nodes are states (lifecycle view) or steps/activities (process view).
//   - Edges are event types. An edge from state A to B means: an event of
//     this type carries the entity from A to B. Name, the data schema and
//     preconditions hang off the edge.
//
// This package only holds the data; generators and validation live elsewhere.
package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"
)

// Kind distinguishes the modelling intent of a draft.
type Kind string

const (
	// KindEntity models a single entity lifecycle (e.g. an Order).
	KindEntity Kind = "entity"
	// KindProcess models a business process (e.g. a Checkout).
	KindProcess Kind = "process"
)

// idPattern constrains the draft id to a URL/file-safe slug.
var idPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// nsPattern allows dotted/hyphenated namespaces like "identity.employee".
var nsPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// Draft is a complete model: the directed graph plus its metadata. It is the
// single source of truth from which schemas and documentation are generated.
type Draft struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind Kind   `json:"kind"`
	// Namespace prefixes generated event-type names, e.g. "order".
	Namespace string `json:"namespace"`
	// SubjectStyle is the subject template, e.g. "/orders/{id}".
	SubjectStyle string `json:"subjectStyle,omitempty"`
	Nodes        []Node `json:"nodes"`
	Edges        []Edge `json:"edges"`
	// Steps is the ordered outline of the process (events and tasks). It is the
	// low-code authoring view; the graph (Nodes/Edges) is the canvas view.
	Steps     []Step    `json:"steps,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// StepKind distinguishes an event (a fact) from a task/command (an action).
type StepKind string

const (
	StepEvent StepKind = "event"
	StepTask  StepKind = "task"
)

// Step is one ordered node of the process outline.
type Step struct {
	ID   string   `json:"id"`
	Kind StepKind `json:"kind"`
	// Name is the event-type name (events) or command name (tasks).
	Name string `json:"name"`
	// Phase is the lifecycle phase for events (active/complete/error/info).
	Phase       string `json:"phase,omitempty"`
	Description string `json:"description,omitempty"`
	// Fields are the data-payload fields (events), from which the JSON Schema
	// is generated.
	Fields []Field `json:"fields,omitempty"`
}

// Field is one data-payload field of an event, authored in the field builder.
type Field struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Type is one of: string, integer, number, boolean, datetime, enum,
	// reference.
	Type        string   `json:"type"`
	Required    bool     `json:"required,omitempty"`
	Format      string   `json:"format,omitempty"` // string format, e.g. uuid, email
	Ref         string   `json:"ref,omitempty"`    // reference target collection
	Enum        []string `json:"enum,omitempty"`
	Description string   `json:"description,omitempty"`
}

// Environment is a saved, switchable working context: a server plus a data
// scope (which subjects, types and id-range to look at). It is the global base
// layer of the scope concept (docs/SCOPE.md §3.1) — the only layer that is
// persisted, reaches Clio and sets the read limit; the shared Queries pipeline
// and per-discipline lenses narrow further on top of it. The token is never
// stored — it stays in the connect flow.
type Environment struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	ServerURL  string   `json:"serverUrl,omitempty"`
	Subject    string   `json:"subject,omitempty"`
	Types      []string `json:"types,omitempty"`
	LowerBound string   `json:"lowerBound,omitempty"`
	UpperBound string   `json:"upperBound,omitempty"`
	Limit      int      `json:"limit,omitempty"` // 0 = use the global cap
}

// Node is a state (lifecycle) or step/activity (process).
type Node struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// Start marks an initial node; End marks a terminal node.
	Start bool `json:"start,omitempty"`
	End   bool `json:"end,omitempty"`
	// X and Y carry the editor layout position (canvas coordinates).
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Edge is an event type: a directed transition between two nodes.
type Edge struct {
	ID string `json:"id"`
	// Type is the event-type name within the namespace, e.g. "shipped".
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	From        string `json:"from"`
	To          string `json:"to"`
	// DataSchema is the JSON Schema of the event's data payload. It is kept
	// as raw JSON so the editor round-trips it untouched; nil/empty means the
	// schema has not been authored yet (flagged by validation, §5.4).
	DataSchema json.RawMessage `json:"dataSchema,omitempty"`
	// Preconditions are free-form invariants noted on the transition.
	Preconditions []string `json:"preconditions,omitempty"`
}

// Validate checks structural integrity of a draft (not the modelling-level
// checks of §5.4, which are computed separately for display).
func (d *Draft) Validate() error {
	if !idPattern.MatchString(d.ID) {
		return fmt.Errorf("invalid draft id %q: must be a slug", d.ID)
	}
	if d.Name == "" {
		return errors.New("draft name must not be empty")
	}
	switch d.Kind {
	case KindEntity, KindProcess:
	default:
		return fmt.Errorf("invalid kind %q", d.Kind)
	}
	if d.Namespace != "" && !nsPattern.MatchString(d.Namespace) {
		return fmt.Errorf("invalid namespace %q", d.Namespace)
	}

	nodeIDs := make(map[string]struct{}, len(d.Nodes))
	for _, n := range d.Nodes {
		if n.ID == "" {
			return errors.New("node id must not be empty")
		}
		if _, dup := nodeIDs[n.ID]; dup {
			return fmt.Errorf("duplicate node id %q", n.ID)
		}
		nodeIDs[n.ID] = struct{}{}
	}

	edgeIDs := make(map[string]struct{}, len(d.Edges))
	for _, e := range d.Edges {
		if e.ID == "" {
			return errors.New("edge id must not be empty")
		}
		if _, dup := edgeIDs[e.ID]; dup {
			return fmt.Errorf("duplicate edge id %q", e.ID)
		}
		edgeIDs[e.ID] = struct{}{}
		if _, ok := nodeIDs[e.From]; !ok {
			return fmt.Errorf("edge %q references unknown source node %q", e.ID, e.From)
		}
		if _, ok := nodeIDs[e.To]; !ok {
			return fmt.Errorf("edge %q references unknown target node %q", e.ID, e.To)
		}
	}
	return nil
}

// ValidID reports whether s is a valid slug identifier.
func ValidID(s string) bool { return idPattern.MatchString(s) }
