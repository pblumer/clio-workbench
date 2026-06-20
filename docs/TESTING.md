# Tests & Coverage

**Stand:** Gesamt-Statement-Coverage **98,4 %** (gemessen mit `go test ./...`).

## Coverage messen

```sh
go test -coverprofile=cover.out ./...
go tool cover -func=cover.out | tail -1        # Gesamtzahl
go tool cover -func=cover.out | grep -v 100.0% # verbleibende Lücken
go tool cover -html=cover.out                  # zeilengenau im Browser
```

## Coverage pro Paket

| Paket | Coverage |
|---|---:|
| `internal/config` | 100,0 % |
| `internal/model` | 100,0 % |
| `internal/bpmngen` | 100,0 % |
| `internal/validate` | 100,0 % |
| `internal/process` | 99,6 % |
| `internal/simulator` | 99,4 % |
| `internal/producergen` | 99,3 % |
| `internal/clio` | 98,2 % |
| `internal/schemagen` | 98,5 % |
| `internal/server` | 97,9 % |
| `internal/envstore` | 97,1 % |
| `internal/testreport` | 96,8 % |
| `internal/scenario` | 95,3 % |
| `internal/store` | 95,2 % |
| `cmd/clio-workbench` | 82,8 % (`run()` 96 %) |

Tests verwenden ausschließlich die Standardbibliothek (`testing`, `net/http/httptest`,
…), tabellengetrieben im Stil der bestehenden Tests. Es gibt **keine** externen
Test-Abhängigkeiten.

## Politik: 100 % wo sauber, der Rest dokumentiert

Ziel war **literal 100 %, wo es ohne Verbiegen des Produktivcodes erreichbar
ist**. Kein Produktivcode wurde nur zwecks Testbarkeit verändert (keine
künstlichen Interfaces/Fehler-Injektionen). Go besitzt keinen eingebauten
Mechanismus, einzelne Zeilen von der Coverage auszunehmen — die verbleibenden,
bewusst ungedeckten Zeilen sind daher hier dokumentiert.

Zwei wiederkehrende Gründe für „echte" Unerreichbarkeit:

- **`json.Marshal`/`MarshalIndent` interner Structs** kann nicht fehlschlagen
  (keine Channels/Funcs/zirkulären Verweise) — der `if err != nil`-Zweig ist
  toter, aber korrekter Defensivcode.
- **Datei-I/O-Fehler** (`WriteFile`/`Write`/`Close`/`Rename`) lassen sich in
  dieser Umgebung nicht provozieren: Die Tests laufen als **root (uid 0)**,
  wodurch `chmod`-basierte Schreibsperren umgangen werden.

## Bewusst nicht abgedeckte Zeilen

### `cmd/clio-workbench/main.go`
- `main()` — dünner `os.Exit(1)`-Wrapper um `run()`; nur per Subprozess-Test
  erreichbar. `run()` selbst ist zu 96 % getestet (Store-/Envstore-Open-Fehler,
  `ListenAndServe`-Fehler, SIGTERM-Graceful-Shutdown).
- `run()`, `server.New`-Fehlerzweig — `New` kann mit den gültigen eingebetteten
  Templates/Static-FS nicht fehlschlagen.

### `internal/server`
- `conformance.go` — `io.ReadAll`-Lesefehler des Multipart-Parts; ein
  In-Memory-Part kann beim Lesen nicht fehlschlagen.
- `editor.go` — die `if !s.saveDraft { return }`-Rückgaben in
  add/update/move/delete. `store.Save` scheitert nur per Validierung (von diesen
  Mutationen nie verletzt) oder per I/O (unmöglich, da dasselbe Verzeichnis beim
  unmittelbar vorausgehenden `Get` lesbar war). `saveDraft` selbst ist über
  `handleSaveMeta` (invalide Namespace) zu 100 % gedeckt.
- `environments.go` — `s.envs.Upsert`-Schreibfehler (Platten-I/O).
- `generator.go` — der `run.JSON()`-Fehlerzweig im Report-Download: ein
  `testreport.Run` ist stets marshalbar (toter Defensivcode).
- `producer.go` — die `producergen.Generate`-Fehlerzweige in `handleProducer`
  und `handleProducerDownload`: die Sprache ist über `pickLang`/`SupportedLang`
  bereits validiert, `Generate` schlägt damit nicht mehr fehl (Defensivcode).
- `scenarios.go` — die `s.scenarios.Save`-Fehlerzweige in
  create-suite/add-case/delete-case und der `s.scenarios.Delete`-Fehler
  (außer `ErrNotFound`) in delete-suite: scheitern nur per I/O (root-uid,
  unmöglich) — analog zu `editor.go`. Validierungs- und Decode-Fehlerpfade
  (`store.Get`/`scenarios.Get`/`List` auf korrupter Datei → 400/500) sind
  gedeckt.
- `inspector.go` — `json.Indent`-Fallback, der gelingt, obwohl das vorherige
  Streaming-Decode fehlschlug: widersprüchlich (beide validieren dasselbe JSON).
- `process.go` — `json.Marshal` eines internen `[]rep`-Structs.
- `server.go` — Setup-Fehlerpfade von `New`/`routes` (`ParseFS`, `fs.Sub`,
  `routes`); mit den eingebetteten FS nicht auslösbar.

### `internal/clio/client.go`
- `readFullEventsURL`/`readEventsURL` — interner `if base == ""`-Recheck und
  `http.NewRequestWithContext`-Fehlerpfad. Alle Aufrufer prüfen `base != ""`
  bereits und bauen die URL aus genau dieser validierten Base; nur über eine
  TOCTOU-Race zwischen zwei `Snapshot()`-Aufrufen erreichbar.
- `AppendEvent` — `http.NewRequestWithContext`-Fehlerpfad; Methode und URL sind
  konstant gültig, daher unerreichbar.

### `internal/store/store.go` (`write`)
- `json.MarshalIndent`-Fehler (`*model.Draft` ist voll marshalbar).
- `tmp.Write`/`tmp.Close`-I/O-Fehler (root-uid, s. o.).

### `internal/envstore/envstore.go` (`save`)
- `json.MarshalIndent`-Fehler (reiner Struct).
- `os.WriteFile`/`os.Rename`-I/O-Fehler (root-uid, s. o.).

### `internal/scenario/store.go`
- `Delete` — der `os.Remove`-Fehler außer `ErrNotExist` (Schreibsperre; root-uid,
  s. o.).
- `write` — `os.CreateTemp`/`tmp.Write`/`tmp.Close`/`os.Rename`-I/O-Fehler
  (root-uid, s. o.). Der `json.MarshalIndent`-Fehler **ist** gedeckt: eine
  `Step.Data` mit ungültigem Roh-JSON lässt ihn fehlschlagen.

### `internal/simulator/simulator.go`
- `pickEdge` — der abschließende `return edges[len(edges)-1]`: `weightOf` ist
  stets ≥ 1, also `total ≥ 1`, und die gewichtete Schleife trifft immer vorher
  zu. Defensiver, toter Rückgabewert (vom Compiler verlangt).

### `internal/producergen/producergen.go`
- `genGo` — der `format.Source`-Fehlerzweig; der erzeugte Go-Code ist stets
  parsebar (toter Defensivcode). Der Test belegt zusätzlich gofmt-Stabilität.

### `internal/testreport/testreport.go`
- `Run.JSON` — `json.MarshalIndent`-Fehler; `Run` ist ein reiner Struct und
  stets marshalbar (toter Defensivcode).

### `internal/schemagen/schemagen.go`
- `SchemaCollection` — `json.MarshalIndent`-Fehler; marshalt `json.RawMessage`
  aus `EventSchema`, das stets valides JSON liefert.

### `internal/process`
- `process.go` (`Discover`) — `if len(seq) == 0 { continue }`; ein Subject landet
  nur mit ≥ 1 Event in der Reihenfolge.
- `references.go` (`BuildReferences`) — `addEdge`-Guard
  `from == "" || to == "" || from == to`; alle Aufrufstellen liefern nicht-leere,
  verschiedene Werte.
