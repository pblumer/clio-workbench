# Workbench-Shell — das UI-Framework

**Status:** `STUFE 0` · **Bezug:** [`WORKBENCH.md`](WORKBENCH.md) §2, §3.2

Die Workbench bekommt mit der Zeit *viele* Diagramme und Werkzeuge. Damit jede
neue Ansicht billig einzuhängen ist, liegt über den Funktionen eine generische
**Schale im VS-Code-Stil**, die aus einer **Contribution-Registry** gespeist
wird. Die Schale rendert das Chrome (Tabs, Sidebar-Einträge, Panes); ein
konkretes Werkzeug ist nur noch eine kleine Deklaration plus ein
Fragment-Handler.

Treu zu den Leitprinzipien (`WORKBENCH.md` §2): ein Binary, `embed.FS`, kein
Build-Step, HTMX + `html/template`, schlankes Vanilla-JS, Space-Look.

## Die Regionen

```
┌──────────────────────────────────────────────────────────────┐
│ Title bar                                                      │
├───┬───────────────┬──────────────────────────────────────────┤
│ A │  Sidebar      │  Editor (Tabs)                            │
│ c │  (zur aktiven │  ┌─[ Event Space ][ Process ][ … ]──────┐ │
│ t │   Activity)   │  │                                       │ │
│ i │               │  │   die Diagramme                       │ │
│ v │               │  ├───────────────────────────────────────┤ │
│ . │               │  │  Panel (Tabs): Konformität · Output   │ │
├───┴───────────────┴──────────────────────────────────────────┤
│ Status bar: Verbindung · Server · Datenablage · Stufe         │
└──────────────────────────────────────────────────────────────┘
```

| Region | `Region`-Wert | Wofür |
|---|---|---|
| Activity bar | — (`Activity`) | Icon-Leiste links; wählt die Sidebar-Gruppe |
| Sidebar | `RegionSidebar` | kontextuelle Panels: Listen, Formulare, Scope |
| Editor | `RegionEditor` | die Diagramme/Analyse-Ansichten als Tabs |
| Panel | `RegionPanel` | Output-artige Werkzeuge (z. B. Konformität) |
| Status bar | — | Verbindung, aktiver Server, Datenablage |

## Das Datenmodell

Die Registry steht in [`internal/server/shell.go`](../internal/server/shell.go):

```go
type Activity struct { ID, Title, Icon string; Views []View } // Activity-Bar-Eintrag
type View     struct { ID, Title, Icon, Body string; Default bool }
```

- `Body` benennt ein Template, das den Inhalt der View rendert.
- Die Schale rendert es über die `partial`-Template-Funktion mit dem
  `shellData`-Wurzelkontext (`.`) — die View kommt so an Draft-Liste, aktiven
  Server usw.
- `Default: true` markiert den zuerst sichtbaren Editor-/Panel-Tab.

## Eine neue Ansicht einhängen (z. B. ein neues Diagramm)

1. **Handler** schreiben, der das Fragment rendert (wie `/space`, `/process`):

   ```go
   s.mux.HandleFunc("GET /heatmap", s.handleHeatmap) // in routes()
   ```

2. **Body-Template** in [`web/templates/views.html`](../web/templates/views.html)
   anlegen. Es lädt sein Fragment per HTMX in einen eigenen Slot — die
   Slot-ID/Trigger sind der Vertrag mit dem Handler:

   ```html
   {{define "view-heatmap" -}}
   <div id="heatmap-slot" class="wb-fill"
        hx-get="/heatmap" hx-trigger="load, scope-changed from:body"
        hx-target="this" hx-swap="innerHTML">…</div>
   {{- end}}
   ```

3. **View registrieren** in `contributions()`:

   ```go
   editor = append(editor, View{ID: "heatmap", Title: "Heatmap",
       Icon: "▦", Body: "view-heatmap"})
   ```

Fertig — die Schale erzeugt Tab und Pane automatisch, und der Eintrag taucht in
der Sidebar „Forschung" als öffnender Link auf.

## Welche Daten eine Ansicht sieht

Die Schale bestimmt, *wo* eine Ansicht lebt; **welche Events** sie liest, regelt
das geschichtete **Scope-Konzept** ([`SCOPE.md`](SCOPE.md)): ein globales
**Environment** (Server + Basis-Scope + Limit), die geteilte **Query-Pipeline**
und — pro Ansicht — eine optionale **Disziplin-Linse**. Eine neue Ansicht erbt
Environment + Queries automatisch, indem sie `scopedEvents(ctx)` aufruft; will
sie zusätzlich lokal verengen (wie Process mit Subject/Source oder der Event
Space mit dem Frame), übergibt sie ihre Linse: `scopedEvents(ctx, lens)`. Details
und der Auflösungsvertrag stehen in [`SCOPE.md`](SCOPE.md).

## Client-Seite

[`web/static/js/shell.js`](../web/static/js/shell.js) schaltet nur Sichtbarkeit
um (Activity-Auswahl, Tab-Wechsel, Sidebar einklappen, Panel auf/zu) und merkt
sich das Layout in `localStorage`. **Inhalte** lädt und aktualisiert
ausschließlich HTMX über die Trigger der Body-Templates
(`scope-changed`/`clio-changed` `from:body`). Die bestehenden Analyse-Skripte
(`process.js`, `dotted.js`, …) hängen unverändert an den wiederverwendeten
Slot-IDs.

## Migration (Stufe 0)

Die bestehenden Funktionen wurden ohne Verhaltensänderung in die Schale
umgezogen, indem ihre Fragment-Slots (`#events-slot`, `#process-slot`,
`#relations-slot`, `#environments-slot`, `#queries-slot`, `#conformance-result`,
`#drafts`, `#inspector`) erhalten blieben:

| Funktion | Region | Activity / Ort |
|---|---|---|
| Modell anlegen + Drafts | Sidebar | Modelle |
| Environment, Queries | Sidebar | Umgebung |
| Event Space, Process, Relationships | Editor | Forschung (Tabs) |
| Konformität, Output | Panel | unten |
| Verbindung + Server-Menü | Status bar | unten |
