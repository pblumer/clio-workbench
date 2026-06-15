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

## Status

**Stufe 0 — Gerüst** (scaffold). What exists today:

- Single Go binary with the UI, templates, CSS and htmx baked in via `embed.FS`.
- File-backed **draft store** (`WORKBENCH_DATA`): drafts are versionable JSON.
- A **start page** in the Clio space-look (starfield + neon HUD) where you
  create and list drafts.
- `/api/*` **reverse proxy** to an upstream Clio with the bearer token injected
  **server-side** (never exposed to the browser) and `FlushInterval: -1` so
  NDJSON/SSE streams are not buffered. Disabled gracefully when no `CLIO_URL`
  is set — the Workbench then runs purely offline on the draft.

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

## Configuration

| Variable          | Required | Default            | Meaning                                   |
|-------------------|----------|--------------------|-------------------------------------------|
| `CLIO_URL`        | no\*     | —                  | Upstream Clio base URL (push & Gegenprobe)|
| `CLIO_API_TOKEN`  | no\*     | —                  | Bearer token, injected server-side        |
| `WORKBENCH_ADDR`  | no       | `:8080`            | Listen address                            |
| `WORKBENCH_DATA`  | no       | `./workbench-data` | Where drafts are stored                   |

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
internal/model/       shared draft data model (directed graph: nodes + event edges)
internal/store/       file-backed draft store (atomic JSON writes)
internal/server/      routing, html/template rendering, /api reverse proxy
web/                  embedded templates, CSS, htmx
docs/WORKBENCH.md     architecture & idea paper
```
