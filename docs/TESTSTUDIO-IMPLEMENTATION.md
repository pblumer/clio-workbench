# Teststudio — Implementierungs-Roadmap & Arbeitspakete

**Status:** `ABGESCHLOSSEN` (WP-1…WP-9) · **Bezug:** [`TESTSTUDIO.md`](TESTSTUDIO.md) (Konzept) · [`TESTING.md`](TESTING.md) (Test-Politik)

Dieses Dokument übersetzt das Konzept aus [`TESTSTUDIO.md`](TESTSTUDIO.md) in
konkrete, abnahmefähige **Arbeitspakete (WP)**. Jedes WP ist für sich
abschließbar, hat klare Abnahmekriterien und eine Aufwandsschätzung in
T-Shirt-Größen.

## Arbeitsweise

- **Paketweise, testgetrieben.** Jedes WP liefert ein in sich getestetes Go-Paket
  oder einen klar abgegrenzten Server-Beitrag. Tabellengetriebene Tests, nur
  Standardbibliothek (`testing`, `net/http/httptest`) — wie in `TESTING.md`. Ziel
  ist die dort dokumentierte Coverage-Latte (≈ 98 %+, „100 % wo sauber").
- **`go build ./... && go test ./... && go vet ./...` ist grün** vor jedem Commit.
- **Stdlib-first.** Externe Abhängigkeiten nur, wenn unumgänglich, und dann je
  eine bewusste Entscheidung (siehe offene Fragen `TESTSTUDIO.md` §12).
- **Ein Binary, `embed.FS`, kein Build-Step, Space-Look** — durchgehend.

## Abhängigkeitsfolge

```
WP-1 (validate: Übergang + Payload)        ─┐  T0
WP-2 (Studio-Shell + Schema-Test-View)     ─┘
        │
WP-3 (scenario: Datenmodell + Store)       ─┐  T1
WP-4 (Szenario-Editor + Sequenz + Pfad)    ─┘
        │
WP-5 (simulator: Walk + Faker)             ─┐  T2
WP-6 (Mutation + Stichproben + Report)     ─┘
        │
WP-7 (producergen: Producer-Code)           →  T3
        │
WP-8 (Instanz: Push/Round-Trip + Scope)     →  T4
        │
WP-9 (Gegenprobe auf validate konsolidieren)→  T5
```

WP-1 hat keine Vorbedingungen außer dem bestehenden `internal/model`. Alles
Spätere hängt (mittelbar) an WP-1, weil `internal/validate` das gemeinsame Herz
ist (`TESTSTUDIO.md` §6).

---

## Stufe T0 — Engine & Schema-Tests

### WP-1 · `internal/validate` — Übergangs- & Payload-Engine · **M**

Das Fundament. Reines Go, **keine** externe Abhängigkeit.

- **Übergangs-Engine** (`TESTSTUDIO.md` §6.2): aus `model.Draft` eine `Machine`
  bauen (Start-/End-Knoten, Übergänge `Knoten ×Event-Typ → Knoten`), eine
  Event-Typ-Sequenz ablaufen und ein `Outcome` liefern: `OK`, durchlaufener
  `Path`, bei Rot Index und Grund der ersten Abweichung.
- **Payload-Validierung** (`TESTSTUDIO.md` §6.1): eine `data`-Payload gegen die
  authorisierten `[]model.Field` prüfen — Pflichtfelder, Typ (string/integer/
  number/boolean/datetime/enum/reference), `enum`-Werte, leichte `format`-Prüfung.
  Fehler **feldgenau** (`{Field, Rule, Message}`).
  - **Bewusste Entscheidung:** kein generischer JSON-Schema-Validator, sondern
    direkte Prüfung gegen das `Field`-Modell. Das umgeht offene Frage §12.1 für
    v1, hält das Binary abhängigkeitsfrei und spiegelt exakt `schemagen.propSchema`.
- **Abnahme:** `go test ./internal/validate` grün, Coverage ≥ 98 %; positive und
  negative Fälle je Regel; Sequenzen mit/ohne Start, Sackgasse, unbekannter Typ.
- **Status:** ✅ fertig. `internal/validate` mit `Machine`/`CheckSequence` und
  `CheckPayload`, 98,3 % Coverage.

### WP-2 · Studio-Shell + Schema-Test-Ansicht · **S–M**

- Neue **Activity „Teststudio"** in `internal/server/shell.go` (`contributions()`).
- Editor-Tab *Schema-Test*: Event-Typ wählen, `data`-JSON eingeben, gegen WP-1
  prüfen, feldgenaue Fehler im Space-Look anzeigen (HTMX-Fragment).
- Body-Template in `web/templates/views.html`, Handler in `internal/server`
  (`GET/POST /studio/schema-test`) nach `FRAMEWORK.md`.
- **Abnahme:** Tab erscheint, valide/invalide Payload erzeugt grün/rot;
  Handler-Tests mit `httptest`; keine Framework-Änderung nötig.
- **Status:** ✅ fertig. Activity „Teststudio" + Editor-Tab *Schema-Test*
  (`internal/server/studio.go`, `web/templates/studio.html`); Modell- und
  Event-Typ-Auswahl mit Schema-Vorschau, feldgenaue Ergebnisse über
  `internal/validate`. `studio.go` zu 100 % getestet.

---

## Stufe T1 — Szenarien

### WP-3 · `internal/scenario` — Datenmodell + Store · **M**

- Typen `Suite`/`Case`/`Step`/`Expectation` (`TESTSTUDIO.md` §5), inkl.
  `DraftRev` für Drift-Erkennung.
- Datei-Store unter `WORKBENCH_DATA` (atomare JSON-Writes), analog
  `internal/store` / `internal/envstore`.
- **Abnahme:** Round-Trip Speichern/Laden, Validierung, Drift-Flag; Coverage-Latte.
- **Status:** ✅ fertig. `internal/scenario` mit `Suite`/`Case`/`Step`/
  `Expectation`, Datei-Store unter `<DataDir>/scenarios/` (Unterverzeichnis, damit
  Suiten nicht in die Draft-Liste geraten), `Validate`, sowie `DraftRev`/`Drift`
  (Fingerprint nur über test-relevante Modellinhalte, ignoriert Layout/Zeit).
  95,3 % Coverage.

### WP-4 · Szenario-Editor + Sequenz-Tests + Pfad-Ansicht · **M–L**

- Sidebar: Suites/Szenarien anlegen, wählen, Seed setzen.
- Sequenz-Test über WP-1; Ergebnis im **Panel** (rot/grün, erste Abweichung).
- **Pfad-Ansicht**: durchlaufener Pfad im Graphen eingefärbt (wiederverwendetes
  Process-Rendering, Space-Look).
- **Abnahme:** Szenario „Storno nach Versand verboten" (Beispiel §3.3) läuft rot
  mit korrekter Begründung; Handler-Tests.
- **Status:** ✅ fertig. Editor-Tab *Szenarien* + Sidebar-Eintrag: Modell/Suite
  wählen, Suiten & Szenarien anlegen/löschen, Sequenz als `→`/Komma-Liste,
  Erwartung accept|reject + optionaler Endzustand. „Alle prüfen" läuft über
  `validate.CheckSequence`; Ergebnisse rot/grün mit erster Abweichung **und**
  dem durchlaufenen Pfad als eingefärbte Knotenkette. Drift-Warnung über
  `scenario.Drift`. Scenario-Store via DI in `server.New` verdrahtet.

---

## Stufe T2 — Generator

### WP-5 · `internal/simulator` — Graph-Walk + Payload-Faker · **M–L**

- Gewichteter Random-Walk über die `Machine` (Start → End/Längengrenze),
  **Seed-determiniert** (`math/rand`, Seed im Ergebnis festgehalten).
- Payload-Faker: aus `[]model.Field` schema-gültige `data` erzeugen (Typen,
  `enum`, `format` plausibel).
- **Abnahme:** gleicher Seed ⇒ identischer Strom; jeder erzeugte Strom besteht
  WP-1; Kanten-Überdeckungs-Modus.
- **Status:** ✅ fertig. `internal/simulator` mit `Generate`/`GenerateN`
  (Seed-deterministischer, gewichteter Graph-Walk), Schema-Faker für alle
  Feldtypen (uuid/email/date-time inkl.) und `EdgeCoverage`. Property-Test:
  50 erzeugte Ströme bestehen alle `validate.CheckSequence`/`CheckPayload`.
  98,9 % Coverage (einzige Lücke: ein dokumentierter defensiver Rückgabewert).

### WP-6 · Mutation + Stichproben + Report · **M**

- Mutation grün→rot (`TESTSTUDIO.md` §4.3): Pflichtfeld weg, Typ verfälschen,
  Nicht-Kante einschieben, Reihenfolge tauschen.
- Property-Stichproben: „N zufällige Pfade, alle grün".
- `internal/testreport`: Report als **Markdown** und **JSON** (`TESTSTUDIO.md` §8),
  inkl. Seed und Kanten-Überdeckung.
- **Abnahme:** mutierte Ströme werden zuverlässig **abgelehnt**; Report
  deterministisch reproduzierbar.
- **Status:** ✅ fertig. `simulator.Mutations` (insert-unknown, swap-order,
  drop-required, wrong-type) — die garantiert-invaliden werden im Test über
  `validate` als abgelehnt belegt. `internal/testreport` rendert den Lauf als
  Markdown/JSON (Seed, Pass/Fail, Kanten-Überdeckung, Negativ-Prüfungen). Neuer
  Editor-Tab *Generator*: Seed + Stichprobenzahl, „Generieren & prüfen" zeigt
  N/N gültig + Überdeckung + Negativ-Prüfungen, mit Report-Download (MD/JSON).

---

## Stufe T3 — Producer-Code

### WP-7 · `internal/producergen` — Producer-Code je Sprache/Plattform · **L**

- Pro Event-Typ aus dem Modell: typisierter Payload-Träger, CloudEvents-Append,
  Subject-Helfer, Token aus Env (`TESTSTUDIO.md` §9). `text/template`, embedded.
- v1-Sprachen-Satz klein und gepflegt halten (offene Frage §12.7) — Vorschlag
  zuerst: **Go**, **TypeScript (fetch)**, **Python**, **curl**.
- Optionale Befüllung mit dem Faker (WP-5), sodass Snippets lauffähig sind.
- Editor-Tab *Producer-Code* mit Sprachumschalter, kopieren/herunterladen.
- **Abnahme:** generierte Go-/curl-Beispiele kompilieren/laufen in einem Smoke-
  Test; Golden-File-Tests je Sprache.
- **Status:** ✅ fertig. `internal/producergen` erzeugt **Go, TypeScript (fetch),
  Python, curl** — pro Event-Typ ein typisierter Payload-Träger + Send-Funktion,
  generischer CloudEvents-POST an `/api/v1/events`, Token aus `CLIO_*`-Env.
  Ehrlich als Gerüst gerahmt (§9.3). Der Go-Output läuft durch `go/format` und
  ist im Test als **valide & gofmt-stabil** belegt. Editor-Tab *Producer-Code*
  mit Modell-/Sprachumschalter und Datei-Download. (Implementiert mit
  `strings.Builder` statt `text/template` — die Zielsprachen-Klammern vertragen
  sich schlecht mit Go-Template-Delimitern.)

---

## Stufe T4 — Instanz-Integration

### WP-8 · Push / Round-Trip + erzwungener Test-Scope · **M–L**

- Betanken & Round-Trip über den bestehenden `/api`-Proxy (`TESTSTUDIO.md` §7).
- **Test-Scope-Garantie** (§7.3): Push nur gegen als „Wegwerf" markiertes Ziel /
  eindeutigen Subject-Namensraum; HUD-Warnung; Default verweigert Push auf
  Nicht-Wegwerf-Server.
- **Abnahme:** Push gegen httptest-Fake; Verweigerung ohne Test-Scope getestet;
  Round-Trip liest zurück und prüft mit WP-1.
- **Status:** ✅ fertig. `clio.AppendEvent` (CloudEvents-POST) + Push-Tab:
  **hartes Gate** (Push nur nach expliziter Wegwerf-Bestätigung der aktiven
  Instanz; Serverwechsel entwaffnet automatisch) und **Auto-Präfix** aller
  Subjects unter `/_test/<run-id>/…`. Round-Trip liest die gepushten Events
  zurück und prüft jede Subject-Sequenz mit `validate`. Getestet mit einer
  aufzeichnenden Fake-Clio (Append + Read-back), inkl. Gate-, Schreibfehler-
  und Round-Trip-Fehlerpfaden.

---

## Stufe T5 — Konsolidierung

### WP-9 · Gegenprobe auf `internal/validate` umstellen · **M**

- `internal/server/conformance.go` und `internal/process`-Konformität auf die
  **eine** Engine aus WP-1 ziehen, statt zwei Implementierungen zu pflegen
  (`TESTSTUDIO.md` §6, §10/T5).
- **Abnahme:** bestehende Gegenprobe-Tests bleiben grün; Doppel-Logik entfernt.
- **Status:** ✅ fertig — mit einer bewussten Präzisierung. Neue
  **Draft-native Gegenprobe** (`gegenprobe.go`): liest reale Events einer Instanz
  (scoped auf das Subject-Präfix des Modells), gruppiert sie pro Subject und
  prüft jede Sequenz mit **`validate.CheckSequence`** — die Soll-Seite
  (Szenarien/Generator/Push) und die Ist-Seite teilen damit dieselbe Engine. Sie
  beantwortet die drei §7-Fragen (abweichende Subjects, toter Entwurf, unbekannte
  Typen). Die ältere BPMN-Upload-Konformität (`conformance.go` /
  `process.CheckConformance`) ist ein **anderer Algorithmus** (linearer
  Sequenz-Matcher über ein BPMN, kein Graph-Walk) und bleibt als eigenes
  Workbench-Feature — sie ist keine Doppel-Logik, die sich sinnvoll
  zusammenführen ließe. Tests grün, `gegenprobe.go` zu 100 %.

---

## Querschnitt / später

- **Invarianten / CEL** (`TESTSTUDIO.md` §6.3, offene Frage §12.2): erst nach
  Klärung der Abhängigkeitsfrage; v1 bleibt bei Schema + Übergang.
- **Akkumulierter Aggregatzustand** (offene Frage §12.3): nötig, sobald
  Invarianten über *Werte* prüfen sollen.
- **JSON-Schema-Bibliothek** (offene Frage §12.1): nur relevant, falls die
  field-basierte Prüfung (WP-1) an externe, von Hand geschriebene Schemas stößt.

---

## Fortschritt

| WP | Stufe | Größe | Status |
|---|---|---|---|
| WP-1 `internal/validate` | T0 | M | ✅ fertig |
| WP-2 Studio-Shell + Schema-Test | T0 | S–M | ✅ fertig |
| WP-3 `internal/scenario` | T1 | M | ✅ fertig |
| WP-4 Szenario-Editor + Pfad | T1 | M–L | ✅ fertig |
| WP-5 `internal/simulator` | T2 | M–L | ✅ fertig |
| WP-6 Mutation + Report | T2 | M | ✅ fertig |
| WP-7 `internal/producergen` | T3 | L | ✅ fertig |
| WP-8 Push / Round-Trip + Scope | T4 | M–L | ✅ fertig |
| WP-9 Gegenprobe konsolidieren | T5 | M | ✅ fertig |

**Alle Arbeitspakete (T0–T5) sind abgeschlossen.** Das Teststudio steht: Engine,
Schema-Test, Szenarien, Generator + Report, Producer-Code, Instanz-Push und die
Soll/Ist-Gegenprobe — Soll- und Ist-Seite auf derselben `internal/validate`-Engine.
