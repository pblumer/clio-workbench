# Clio Workbench

> A drawing board for event-sourcing models. The Workbench helps developers
> *design* the events of an entity or process before they exist — and turns the
> design into usable artifacts: Clio schemas and documentation.

Companion tool to [`pblumer/clio`](https://github.com/pblumer/clio). It talks to
a Clio instance only over its **public HTTP API**, runs as a **single Go
binary** with everything embedded (no npm, no toolchain, no CDN), and is
released independently of Clio. It does **not** replace Clio's embedded `/ui`
(operations dashboard) and does not touch ADR-020.

The full architecture and idea paper lives in [`docs/WORKBENCH.md`](docs/WORKBENCH.md).

![The process viewer: the discovered directly-follows graph of real events, with lifecycle-phase colours, counts, timeline replay and node inspection.](docs/images/process-viewer.svg)

## Status

**Stufe 0 — Gerüst** (scaffold). What exists today:

- A **VS-Code-style shell** (activity bar · sidebar · editor tabs · bottom panel
  · status bar) over the space look, driven by a declarative **contribution
  registry** so new diagrams and tools plug in with one `View` entry plus a
  fragment handler. See [`docs/FRAMEWORK.md`](docs/FRAMEWORK.md).
- Single Go binary with the UI, templates, CSS and htmx baked in via `embed.FS`.
- File-backed **draft store** (`WORKBENCH_DATA`): drafts are versionable JSON.
- A **start page** in the Clio space-look (starfield + neon HUD) where you
  create and list drafts.
- `/api/*` **reverse proxy** to an upstream Clio with the bearer token injected
  **server-side** (never exposed to the browser) and `FlushInterval: -1` so
  NDJSON/SSE streams are not buffered. Disabled gracefully when no `CLIO_URL`
  is set — the Workbench then runs purely offline on the draft.
- A **live connection status** in the header that reports whether Clio is
  actually reachable and the token accepted — not merely whether `CLIO_URL` is
  configured (see below).
- **A layered scope concept** (`docs/SCOPE.md`): every analysis panel reads
  events through three composed layers — a global, persistent **Environment**
  (server + base scope + read limit), the shared session **Queries** pipeline,
  and a per-panel **discipline lens**. Global to define, local to shape; each
  layer only narrows, and only the Environment reaches Clio and sets the limit.
- **Environments**: saved, switchable working contexts — a server plus a data
  scope (subject prefix, event types, lower/upperBound, per-env event limit).
  The active scope applies to all analysis panels; the token is never stored.
  Limits are shown prominently and the header blinks a warning when the store
  holds more events than the active limit.
- A rudimentary **events view**: the event types written to Clio
  (`read-event-types`) rendered as BPMN **send tasks** with an attached data
  object, a per-type count bubble, and a header bubble summing all occurrences.
- A **query pipeline**: after the environment, chain refinement queries — each
  stage (subject prefix, types, id-bounds) further *decimates* the survivors of
  the one before it. The funnel shows the surviving event/subject count per
  stage and feeds every analysis panel below.
- A **dynamic Event Space**: a *frame* keeps the last N events, dots are
  coloured by **event type** (with a type legend), a **live** toggle streams new
  events in over SSE (the Workbench tails Clio server-side), and hovering a dot
  opens a card with the event's metadata and pretty-printed payload. An in-panel
  **filter** narrows *which* events are charted — click a type in the legend to
  toggle it, or type the same filter directly (`type:… subject:… from:… to:…`,
  plus free text matched against type/subject). It is view-only and transient:
  the environment and the query pipeline stay untouched.

Still ahead per the roadmap (`docs/WORKBENCH.md` §8): the drawing canvas and
state-machine view (Stufe 1), event-type schema editor and export (Stufe 2),
BPMN view and schema push (Stufe 3), and the Soll/Ist Gegenprobe (Stufe 4).

## Quick start

```sh
go run ./cmd/clio-workbench
# open http://localhost:8080
```

To enable push and the (later) conformance check against a running Clio:

```sh
CLIO_URL=http://localhost:3000 CLIO_API_TOKEN=… go run ./cmd/clio-workbench
```

## Connection status

The header pill shows the **real** state of the link to Clio, not just whether
`CLIO_URL` is set. The Workbench probes Clio with a single lightweight,
authenticated request (`GET /api/v1/read-event-types`), so reachability **and**
the bearer token are verified together. The bearer token stays strictly
server-side — it never appears in the rendered fragment or in browser JS.

| Status         | Pill (colour)         | Meaning                                            |
|----------------|-----------------------|----------------------------------------------------|
| `online`       | UPLINK (green)        | Reachable **and** token accepted (HTTP 2xx)        |
| `unauthorized` | AUTH FAIL (yellow)    | Reachable but token rejected (HTTP 401/403)        |
| `unreachable`  | UNREACHABLE (red)     | Network/DNS/connection error, timeout, or 5xx      |
| `offline`      | OFFLINE (grey)        | No `CLIO_URL` configured — drafting works offline  |

The status loads on page open (`hx-get="/connection"`) and a **⟳ reconnect**
button re-probes on demand. A non-blocking probe also runs at startup and is
logged (`clio connection check`); it never fails the server, so offline drafting
stays possible even when Clio is down.

### Picking a server in the GUI

You can start the Workbench with no configuration and **choose a Clio server at
runtime** from the *Clio server* panel: enter the URL and (optionally) a token
and press **Connect**; **Disconnect** clears it. The target is held in memory
and applies to both the status probe and the `/api` proxy. `CLIO_URL` /
`CLIO_API_TOKEN` (below) merely **seed** the initial target, so the env-based
flow keeps working.

The token is posted once to the local backend and kept **server-side** — it is
never rendered back into the page or stored in browser JS. The selection is not
persisted across restarts (re-pick after a restart).

> The probe uses an authenticated read op rather than Clio's unauthenticated
> health endpoint (`GET /api/v1/ping`), because `ping` would not exercise the
> token.

### Smoke test

```sh
# Offline: no upstream configured → grey OFFLINE pill.
go run ./cmd/clio-workbench
curl -s localhost:8080/connection

# Online / unauthorized: point at a running Clio.
CLIO_URL=http://localhost:3000 CLIO_API_TOKEN=<valid>  go run ./cmd/clio-workbench  # → green UPLINK
CLIO_URL=http://localhost:3000 CLIO_API_TOKEN=<wrong>  go run ./cmd/clio-workbench  # → yellow AUTH FAIL

# Unreachable: a URL with nothing listening → red UNREACHABLE.
CLIO_URL=http://localhost:3999 CLIO_API_TOKEN=x go run ./cmd/clio-workbench
```

## Configuration

| Variable          | Required | Default            | Meaning                                   |
|-------------------|----------|--------------------|-------------------------------------------|
| `CLIO_URL`        | no\*     | —                  | Upstream Clio base URL (push & Gegenprobe)|
| `CLIO_API_TOKEN`  | no\*     | —                  | Bearer token, injected server-side        |
| `WORKBENCH_ADDR`  | no       | `:8080`            | Listen address                            |
| `WORKBENCH_DATA`  | no       | `./workbench-data` | Where drafts are stored                   |
| `WORKBENCH_SERVERS` | no     | `https://clio.blumer.cloud` | Preset Clio URLs for the connect menu |
| `WORKBENCH_EVENT_CAP` | no   | `50000`            | Max events the analysis panels read from Clio |

\* Without `CLIO_URL`/token the Workbench works offline on the draft; only push
and the Gegenprobe need an instance.

## Development

```sh
go build ./...
go test ./...
go vet ./...
```

### Layout

```
cmd/clio-workbench/   entrypoint (HTTP server, graceful shutdown)
internal/config/      environment configuration
internal/clio/        HTTP client against Clio's public API (connection probe)
internal/model/       shared draft data model (directed graph: nodes + event edges)
internal/store/       file-backed draft store (atomic JSON writes)
internal/server/      routing, html/template rendering, /api reverse proxy, /connection
                      shell.go: the VS-Code shell's contribution registry
web/                  embedded templates, CSS, htmx
                      templates/views.html: contributed View bodies
                      static/js/shell.js: shell chrome (tabs, sidebar, panel)
docs/WORKBENCH.md     architecture & idea paper
docs/TESTSTUDIO.md    the Test Studio: architecture & idea paper (testing models)
docs/FRAMEWORK.md     the UI framework (shell regions + how to add a view)
docs/SCOPE.md         the scope concept (environment · queries · discipline lens)
```
