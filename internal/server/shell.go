package server

// shell.go defines the Workbench's UI framework: a VS-Code-style shell
// (activity bar · sidebar · editor · panel · status bar) driven by a
// declarative *contribution registry*.
//
// The idea — see docs/FRAMEWORK.md — is that the shell chrome is generic and
// every concrete tool (a diagram, an analysis view, an editor) is a small
// `View` that declares *where* it lives and *which template* renders its body.
// Adding a new diagram is then: write a fragment handler, define its body
// template, and append one `View` to contributions(). The shell wires the
// tabs, sidebar entries and panes automatically.
//
// Migration note: the body templates deliberately reuse the existing fragment
// slots (#events-slot, #process-slot, …) and their htmx triggers, so the
// analysis fragments and their JS keep working unchanged inside the new chrome.

// Region places a contributed View into one of the shell's docking areas.
type Region string

const (
	// RegionSidebar — a panel in the collapsible left sidebar, grouped under
	// the activity it belongs to.
	RegionSidebar Region = "sidebar"
	// RegionEditor — a document tab in the central editor area. This is where
	// the diagrams live.
	RegionEditor Region = "editor"
	// RegionPanel — a tab in the bottom panel (output-style tools).
	RegionPanel Region = "panel"
)

// Activity is an entry in the vertical activity bar. Selecting it reveals its
// sidebar Views.
type Activity struct {
	ID    string // stable id, used by the shell JS and DOM
	Title string // shown as the sidebar header
	Icon  string // single glyph for the activity bar (no CDN, unicode only)
	Views []View // the sidebar Views revealed when this activity is active
}

// View is a single contributed piece of UI. Its Body names a template that
// renders the View's content; that template receives the shellData root, so it
// can reach the draft list, the active server, etc.
type View struct {
	ID      string // stable id; drives tab/pane wiring
	Title   string // tab / sidebar caption
	Icon    string // optional glyph for the tab or sidebar list
	Body    string // template name rendered for this View's content
	Default bool   // editor/panel: the tab shown first
}

// shellData is the view model for the shell (index.html). It embeds the
// existing start-page data and adds the resolved contribution registry.
type shellData struct {
	indexData
	Activities []Activity // activity bar + their sidebar Views
	Editor     []View     // editor-area tabs (the diagrams)
	Panel      []View     // bottom-panel tabs
	Stufe      string     // roadmap stage badge for the status bar
}

// contributions returns the static registry of Views that make up the
// Workbench today. This is the single place to extend the UI: append a View
// (and its body template + handler) and it appears in the shell.
//
// Editor Views are the "many diagrams and possibilities" the Workbench is
// meant to grow — each is independent and only needs its fragment endpoint.
func contributions() ([]Activity, []View, []View) {
	activities := []Activity{
		{
			ID: "explore", Title: "Forschung", Icon: "◎",
			Views: []View{
				{ID: "explore-nav", Title: "Analyse-Ansichten", Body: "view-explore-nav"},
			},
		},
		{
			ID: "models", Title: "Modelle", Icon: "▤",
			Views: []View{
				{ID: "models", Title: "Modell anlegen", Body: "view-models"},
			},
		},
		{
			ID: "scope", Title: "Umgebung", Icon: "⛁",
			Views: []View{
				{ID: "environments", Title: "Environment", Body: "view-environments"},
				{ID: "queries", Title: "Queries", Body: "view-queries"},
			},
		},
		{
			ID: "studio", Title: "Teststudio", Icon: "⚗",
			Views: []View{
				{ID: "studio-nav", Title: "Teststudio", Body: "view-studio-nav"},
			},
		},
	}

	editor := []View{
		{ID: "space", Title: "Event Space", Icon: "✦", Body: "view-space", Default: true},
		{ID: "process", Title: "Process", Icon: "❖", Body: "view-process"},
		{ID: "relations", Title: "Relationships", Icon: "⇄", Body: "view-relations"},
		{ID: "schema-test", Title: "Schema-Test", Icon: "✓", Body: "view-schema-test"},
		{ID: "scenario-test", Title: "Szenarien", Icon: "❏", Body: "view-scenario-test"},
		{ID: "generator", Title: "Generator", Icon: "⚙", Body: "view-generator"},
		{ID: "producer", Title: "Producer-Code", Icon: "⌗", Body: "view-producer"},
	}

	panel := []View{
		{ID: "conformance", Title: "Konformität", Icon: "✓", Body: "view-conformance", Default: true},
		{ID: "output", Title: "Output", Icon: "≡", Body: "view-output"},
	}

	return activities, editor, panel
}
