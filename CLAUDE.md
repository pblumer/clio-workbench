# CLAUDE.md — Agenten-Briefing für die Clio Workbench

> Dieser Text konfiguriert jede neue KI-Session (Claude Code & andere Agenten)
> auf dieses Repo. Claude Code liest ihn beim Start **automatisch**; für andere
> Werkzeuge lässt er sich als Eröffnungs-Prompt einkopieren. Halte ihn aktuell —
> er ist die kürzeste verbindliche Beschreibung dessen, *wie* hier gearbeitet
> wird. Das *Warum* steht ausführlich in [`docs/`](docs/).

## Was dieses Projekt ist

Die **Clio Workbench** ist ein Zeichenbrett für Event-Sourcing-Modelle und ein
Labor zum Erforschen echter Event-Ströme. Sie ist das Begleitwerkzeug zu
[`pblumer/clio`](https://github.com/pblumer/clio), spricht eine Clio-Instanz
**ausschließlich über deren öffentliche HTTP-API** an, läuft als **ein einziges
Go-Binary** und wird unabhängig von Clio released. Sie ersetzt **nicht** Clios
eingebettetes `/ui` und berührt **ADR-020 nicht**.

Vollständige Architektur: [`docs/WORKBENCH.md`](docs/WORKBENCH.md).

## Leitprinzipien (nicht verhandelbar)

Diese sechs Prinzipien (`docs/WORKBENCH.md` §2) entscheiden jede technische
Wahl. Eine Änderung, die eines verletzt, ist falsch — auch wenn sie sonst
funktioniert.

1. **Ein Binary, reines Go.** Alles — UI, Assets, Templates, JS, CSS — über
   `embed.FS` ins Binary gebacken. **Kein npm, keine Toolchain, kein CDN.**
2. **Kein Build-Step im Frontend.** HTMX + `html/template` für die Werkbank,
   schlankes Vanilla-JS nur fürs Canvas. Logik so weit wie möglich in Go.
3. **Nur die öffentliche API.** Ausschließlich dokumentierte HTTP-Endpunkte von
   Clio. Kein privilegierter Zugriff, keine Kopplung an Interna.
4. **Der Token bleibt serverseitig.** Der Bearer-Token wird vom Backend gehalten
   und durchgereicht — er landet **nie** im Browser-JS oder im gerenderten HTML.
5. **Der Entwurf ist das Artefakt.** Das Modell ist die Quelle der Wahrheit;
   Schemas und Doku werden daraus *generiert*, nicht umgekehrt.
6. **Space-Look.** Visuell im selben Sci-Fi/HUD-Register wie Clios `/ui`
   (Sternenfeld, Neon-Glow) — gemeinsame Designsprache ohne Code-Kopplung.

## Harte Regeln, die daraus folgen

- **Keine neuen Abhängigkeiten** ohne sehr guten Grund. Produktivcode und Tests
  nutzen die **Go-Standardbibliothek**. `go.mod` bleibt schlank; insbesondere
  **keine** Test-Frameworks (`testing` + `net/http/httptest` genügen).
- **Keine Build-Pipeline, kein Bundler, kein Transpiler.** Wenn etwas einen
  Build-Step bräuchte, gehört es nicht ins Frontend.
- **Token niemals exponieren.** Beim Bauen von Fragmenten oder JS prüfen, dass
  weder Token noch Secrets in die Antwort gelangen.
- **Funktioniert offline.** Ohne `CLIO_URL`/Token muss das Entwerfen am Draft
  weiterlaufen; nur Push und Gegenprobe brauchen eine Instanz.

## Aufbau

```
cmd/clio-workbench/   Einstiegspunkt (HTTP-Server, Graceful Shutdown)
internal/config/      Konfiguration aus der Umgebung
internal/clio/        HTTP-Client gegen Clios öffentliche API
internal/model/       gemeinsames Draft-Datenmodell (gerichteter Graph)
internal/store/        datei-gestützter Draft-Store (atomare JSON-Writes)
internal/envstore/    persistente Environments (environments.json)
internal/scenario/    Test-Studio-Szenarien (Suites/Cases)
internal/validate/    geteilte Validierungs-Engine (Szenarien + Gegenprobe)
internal/process/     Process Discovery (Directly-Follows-Graph, Referenzen)
internal/simulator/   Event-Strom-Generator
internal/schemagen/   Modell → Clio-Schema-Sammlung
internal/producergen/ Modell → Producer-Code
internal/bpmngen/     Modell → BPMN-Rendering
internal/testreport/  Test-Report-Modell
internal/server/      Routing, Template-Rendering, /api-Proxy, /connection
                      shell.go: die VS-Code-Schale (Contribution-Registry)
web/                  eingebettete Templates, CSS, htmx, Vanilla-JS
```

## Tragende Konzepte (vor dem Coden lesen)

- **UI-Framework** ([`docs/FRAMEWORK.md`](docs/FRAMEWORK.md)) — die VS-Code-artige
  Schale und wie man mit *einem* `View`-Eintrag plus Fragment-Handler eine neue
  Ansicht einhängt. Neue Diagramme/Werkzeuge folgen genau diesem Muster.
- **Scope** ([`docs/SCOPE.md`](docs/SCOPE.md)) — *welche* Events eine Ansicht
  liest, regeln drei komponierte Lagen (**Environment → Queries → Disziplin-Linse**).
  Invarianten: Komposition durch Schnittmenge; jede Lage darf nur **verengen**;
  **nur das Environment** erreicht Clio und setzt das Limit. Alle Ansichten lesen
  durch dieselbe Naht: `scopedEvents(ctx, lens...)` in `internal/server/queries.go`.
- **Test Studio** ([`docs/TESTSTUDIO.md`](docs/TESTSTUDIO.md)) — prüft den Entwurf
  ausführend; teilt sich mit der Gegenprobe **eine** Validierungs-Engine
  (`internal/validate`). Soll und Ist laufen auf demselben Code.

## Arbeitsweise

```sh
go build ./...
go test ./...
go vet ./...
go run ./cmd/clio-workbench   # http://localhost:8080
```

- **Tests sind Pflicht.** Die Coverage ist hoch (~98 %, siehe
  [`docs/TESTING.md`](docs/TESTING.md)) und soll es bleiben. Schreibe Tests
  tabellengetrieben im Stil der vorhandenen, **nur mit der Standardbibliothek**.
  Verbiege **keinen** Produktivcode nur zwecks Testbarkeit (keine künstlichen
  Interfaces/Fehler-Injektionen); bewusst ungedeckte Zeilen werden in
  `docs/TESTING.md` begründet.
- **Vor jedem Commit:** `go build ./... && go test ./... && go vet ./...` müssen
  grün sein.
- **Doku ist lebendig.** Verschiebst du Verhalten oder Architektur, ziehe das
  passende Dokument in `docs/` (und ggf. README) mit. Die Kern-Doku ist auf
  **Deutsch**; halte diesen Ton und Stil. README ist Englisch.
- **Im Bestand bleiben.** Lies das umgebende Paket und passe dich Benennung,
  Kommentar-Dichte und Idiom an, statt einen neuen Stil einzuführen.
- **Roadmap-Bewusstsein.** Das Projekt folgt Clios Stufenlogik
  (`docs/WORKBENCH.md` §8). Ordne neue Arbeit in die richtige Stufe ein.

## Git-Konventionen

- Entwickle auf einem Feature-Branch; pushe **nie** ungefragt auf `main`.
- Aussagekräftige Commit-Messages im Stil der Historie (`feat(studio): …`,
  `examples(teststudio): …`, `docs: …`).
- Einen Pull Request nur anlegen, wenn ausdrücklich darum gebeten wird.
</content>
</invoke>
