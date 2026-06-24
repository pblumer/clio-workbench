# Clio Workbench — Teststudio · Architektur- & Ideenpapier

**Status:** `KONZEPT` · **Version:** 0.1 · **Bezug:** [`WORKBENCH.md`](WORKBENCH.md) §4, §6, §7 · [`FRAMEWORK.md`](FRAMEWORK.md)

> Ein Prüfstand für Event-Sourcing-Modelle. Das Teststudio *belebt* den in der
> Workbench gezeichneten Entwurf: Es erzeugt modellkonforme (und bewusst
> kaputte) Event-Ströme, prüft Payloads und Übergänge gegen das Modell, spielt
> benannte Szenarien durch und kann eine Wegwerf-Instanz mit Fixtures betanken.

---

## 1. Motivation & Abgrenzung

Die Workbench (`WORKBENCH.md`) ist *gestaltend*: Man zeichnet den Lebenszyklus,
definiert Event-Typen als Übergänge, hängt JSON-Schemas an die `data`-Payloads
und exportiert daraus Clio-Schemas und Dokumentation. Zwischen *Entwurf* und
*Produktion* fehlt aber ein Schritt: **Stimmt der Entwurf überhaupt?** Schreibt
ein Producer Payloads, die zum Schema passen? Sind die real auftretenden
Übergänge die, die ich gezeichnet habe? Was passiert, wenn ein Event in der
falschen Reihenfolge kommt?

Heute beantwortet die Workbench davon nur zwei Randstücke:

- **Statische Validierung** (`WORKBENCH.md` §5.4) prüft den *Graphen* selbst —
  Erreichbarkeit, Sackgassen, fehlende Schemas, Namenskonflikte. Sie führt
  nichts aus.
- **Gegenprobe / Soll-Ist** (`WORKBENCH.md` §7) vergleicht den Entwurf gegen
  **reale** Events einer **laufenden** Instanz. Sie braucht also bereits
  Produktionsdaten und sagt nichts, *bevor* es die gibt.

Das **Teststudio** schließt die Lücke dazwischen. Es ist *ausführend* und
*hypothetisch*: Es erzeugt Event-Ströme selbst, lässt sie durch dieselbe
Validierungslogik laufen, mit der man später das Ist prüft, und macht Aussagen
über den Entwurf, **ohne** dass schon eine reale Historie existieren muss.

| | Validierung (§5.4) | **Teststudio** (dieses Papier) | Gegenprobe (§7) |
|---|---|---|---|
| Frage | Ist der Graph in sich stimmig? | Verhält sich das Modell wie gedacht? | Entspricht das Ist dem Soll? |
| Eingabe | nur das Modell | Modell + **erzeugte/eingegebene** Streams | Modell + **reale** Events |
| Ausführung | keine (rein strukturell) | simuliert/durchgespielt | gegen Live-Instanz |
| Braucht Instanz? | nein | nein (optional zum Betanken) | ja |
| Zeitpunkt | beim Zeichnen | vor der ersten Zeile Producer-Code | im Betrieb |

Das Teststudio **ersetzt** weder Validierung noch Gegenprobe — es ist die
mittlere Stufe und teilt sich mit der Gegenprobe bewusst die **Validierungs-
Engine** (Abschnitt 6). Wer das Soll prüfen kann, prüft auch das Ist mit
demselben Code.

### 1.1 Das Teststudio als Labor

Die Workbench versteht sich auch als **Labor** (`WORKBENCH.md` §1.1) — ein Ort,
an dem man Hypothesen über Event-Daten bildet und prüft. Das Teststudio ist das
naheliegendste Laborgerät: Es macht aus „ich glaube, der Ablauf sieht so aus"
ein wiederholbares Experiment. Man stellt eine Behauptung auf (ein Szenario),
führt sie aus und bekommt ein reproduzierbares Ergebnis — bei gleichem
**Seed** Bit für Bit dasselbe.

---

## 2. Leitprinzipien

Treu zu den Leitprinzipien der Workbench (`WORKBENCH.md` §2):

1. **Ein Binary, reines Go.** Generator, Validierungs-Engine, Schema-Prüfung,
   Reporting — alles in Go, über `embed.FS` ins Binary gebacken. Kein npm, kein
   CDN.
2. **Kein Build-Step im Frontend.** HTMX + `html/template` für das Studio-UI,
   schlankes Vanilla-JS nur fürs Abspielen/Visualisieren.
3. **Nur die öffentliche API.** Das Betanken einer Instanz und das Zurücklesen
   laufen ausschließlich über Clios dokumentierte HTTP-Endpunkte und den
   bestehenden `/api`-Reverse-Proxy.
4. **Der Token bleibt serverseitig.** Wie überall in der Workbench.
5. **Der Entwurf ist die Messlatte.** Tests werden *gegen das Modell* formuliert;
   das Modell bleibt die Quelle der Wahrheit. Tests sind ein zweites,
   versionierbares Artefakt daneben — nie eine Kopie des Modells.
6. **Reproduzierbar vor allem.** Jeder Lauf ist über einen expliziten **Seed**
   deterministisch. Ein roter Lauf lässt sich exakt wiederholen, ein Report
   ist ein verlässliches Artefakt, kein Zufallsbild.
7. **Lokal zuerst, Instanz optional.** Schema- und Übergangsprüfung laufen rein
   offline am Entwurf. Eine Clio-Instanz braucht nur, wer echt betanken oder
   Clios serverseitige Ablehnung mitprüfen will (Abschnitt 7).
8. **Space-Look.** Visuell konsistent mit `/ui` und Workbench (Sternenfeld,
   Neon, HUD).

---

## 3. Was sich testen lässt — die vier Prüfarten

Das Studio bündelt vier Prüfarten über *demselben* Modell. Sie sind unabhängig
nutzbar und bauen aufeinander auf.

### 3.1 Schema-Tests (Payload ↔ `data`-Schema)

Jeder Event-Typ trägt ein JSON-Schema für seine `data` (`WORKBENCH.md` §6.1).
Der Schema-Test stellt die einfachste Frage: **Passt eine konkrete Payload zum
Schema?**

- Positiv: ein per Hand eingegebenes oder generiertes `data`-Objekt validiert
  grün.
- Negativ: ein bewusst falsches Objekt (fehlendes Pflichtfeld, falscher Typ,
  Format-Verstoß) muss rot werden — und der Fehler muss *zeigen, warum*.

Das ist die Basis jeder höheren Prüfung und läuft komplett lokal.

### 3.2 Übergangs- & Sequenz-Tests (Stream ↔ Graph)

Eine Sequenz von Event-Typen wird gegen den Lifecycle-Graphen gehalten:

- Beginnt die Sequenz an einem **Startzustand**?
- Ist jeder Übergang eine **existierende Kante** vom aktuellen Zustand?
- Endet die Sequenz in einem **gültigen** (ggf. End-)Zustand?
- Wird die **Kardinalität pro Subject** der durchlaufenen Kanten eingehalten
  (Abschnitt 6.3)?
- Werden **Invarianten/Preconditions** der durchlaufenen Kanten eingehalten
  (Abschnitt 6.4)?

Ergebnis ist nicht nur grün/rot, sondern der **durchlaufene Pfad** im Graphen
(im Studio einfärbbar — Space-Look) und die Stelle, an der eine Sequenz
ausbricht.

### 3.3 Szenario-Tests (benannte, versionierbare Fälle)

Ein **Szenario** ist ein benannter, erwartungsbehafteter Fall — das, was man
git-en und im Team teilen will:

```
Szenario "Storno nach Versand ist verboten"
  gegeben:  order-created → order-paid → order-shipped
  wenn:     order-cancelled
  erwartet: ABGELEHNT (kein Übergang von 'shipped' via cancelled)
```

Ein Szenario bündelt eine Sequenz (3.2) und ggf. konkrete Payloads (3.1) mit
einer **Erwartung**: `gültig` / `ungültig` / „Endzustand ist X" / „Invariante Y
hält". Szenarien werden zu **Suites** gruppiert und sind das primäre Artefakt
des Studios (Abschnitt 5).

### 3.4 Generator / Simulator (synthetische Ströme)

Der Generator läuft den Graphen ab und erzeugt **modellkonforme** Event-Ströme
samt schemakonformer Payloads (Abschnitt 4) — und auf Wunsch gezielt
**nicht-konforme** Ströme als Negativ-Material:

- Demodaten und **Fixtures** für eine frische Instanz (Betanken, Abschnitt 7).
- Stichproben-Tests: „erzeuge 1000 zufällige Pfade, jeder muss grün sein" —
  ein leichtes property-based Testing über dem Graphen.
- Last-/Volumendaten, um die *eigenen* Analyse-Panels (Event Space, Process,
  Relationships) mit realistischem Material zu füttern.

---

## 4. Der Generator (Herzstück)

Der Generator ist zum Teststudio, was das Canvas zur Workbench ist. Er macht aus
dem statischen Graphen einen **Strom**.

### 4.1 Pfad-Erzeugung (Graph-Walk)

- Start an einem markierten Startzustand, dann gewichteter Random-Walk entlang
  der ausgehenden Kanten bis zu einem Endzustand oder einer Längengrenze.
- **Kantengewichte** (optional am Modell oder im Test gesetzt) steuern die
  Verteilung — so lassen sich seltene Pfade gezielt häufiger ziehen.
- **Abbruchkriterien**: max. Pfadlänge, max. Subjects, Erschöpfung der
  Kantenüberdeckung („walk bis jede Kante mindestens einmal vorkam").

### 4.2 Payload-Erzeugung (Schema-Faker)

Pro durchlaufener Kante wird eine `data`-Payload aus deren JSON-Schema
synthetisiert: Pflichtfelder gefüllt, Typen/Formate/Enums respektiert, Strings
über `format` (z.B. `email`, `date-time`) plausibel besetzt. Quelle der
Wahrheit ist das Schema des Entwurfs — derselbe Schema-Begriff wie beim Export
(`internal/schemagen`).

### 4.3 Negativ-Erzeugung (Mutation)

Aus einem grünen Strom werden durch gezielte **Mutationen** rote erzeugt:
Pflichtfeld entfernen, Typ verfälschen, einen Übergang einschieben, der keine
Kante ist, die Reihenfolge vertauschen. So entsteht Test-Material, das beweist,
dass die Prüfung auch *ablehnt* — nicht nur durchwinkt.

### 4.4 Determinismus

Jeder Lauf nimmt einen **Seed** (Default: Zeit, aber im Report festgehalten).
Gleicher Seed + gleiches Modell ⇒ identischer Strom. Das ist die Grundlage von
Reproduzierbarkeit und Reporting (Abschnitt 8) und kommt direkt aus
`math/rand`s seedbarer Quelle — keine externe Abhängigkeit.

---

## 5. Datenmodell der Tests

Tests sind ein **eigenes** versionierbares Artefakt neben dem Draft, nicht Teil
des Modells. Sie referenzieren das Modell, kopieren es aber nicht.

```go
// Skizze — finale Form offen (Abschnitt 12)
type Suite struct {
    ID, Name   string
    DraftID    string      // welches Modell wird geprüft
    DraftRev   string      // gegen welchen Stand (Drift erkennen)
    Cases      []Case
}

type Case struct {
    ID, Name   string
    Steps      []Step      // Sequenz von Event-Typen (+ optional Payload)
    Expect     Expectation // Accept | Reject | EndState(x) | Invariant(y)
    Seed       int64       // für generierte Anteile
}

type Step struct {
    Type    string          // Event-Typ = Kante im Graphen
    Subject string          // optional; default: ein generiertes Subject
    Data    json.RawMessage // optional; sonst aus dem Schema gefakt
}
```

- **Persistenz**: Dateien unter `WORKBENCH_DATA` neben den Drafts, als
  versionierbares JSON — git-freundlich, konsistent mit `internal/store` /
  `internal/envstore`.
- **Drift-Erkennung**: `DraftRev` hält fest, gegen welchen Modellstand eine
  Suite geschrieben wurde. Ändert sich das Modell, kann das Studio warnen, dass
  Szenarien neu zu prüfen sind (etwa: eine Kante, auf die sich ein Negativ-Test
  verlässt, existiert nun doch).

---

## 6. Die Validierungs-Engine (geteilt mit der Gegenprobe)

Der Kern, gegen den **alles** läuft — generierte Ströme, eingegebene Szenarien
und (in §7 der Workbench) reale Events. Drei Schichten:

### 6.1 Schema-Validierung

Ein JSON-Schema-Validator in reinem Go prüft `data` gegen das Schema des
Event-Typs. Auswahlkriterium wie bei den Diagramm-Bibliotheken: per `embed.FS`
mitlieferbar, kein CDN, kompatible Lizenz, möglichst aus der Standardbibliothek
oder einer einzelnen, leichten Abhängigkeit. Fehler müssen **lokalisiert**
sein (welches Feld, welche Regel), nicht nur „invalid".

### 6.2 Übergangs-Validierung

Reiner Graphlauf über das `internal/model`-Datenmodell (Knoten + Event-Kanten):
aktueller Zustand → erlaubte Kanten → Folgezustand. Diese Logik ist klein und
existiert in Verwandtschaft schon im Process-/Relations-Code; das Studio
formalisiert sie als prüfbare Maschine.

### 6.3 Kardinalität pro Subject

Manche Event-Typen treten je Subject **genau einmal** auf (Lifecycle-/Anlage-
Events wie `…new.v2`), andere **beliebig oft** (ein Messwert eines Fühlers, eine
Profiländerung). Diese Unterscheidung ist *keine* Schema-Frage — ein JSON-Schema
validiert immer nur ein **einzelnes** Event. Sie ist eine **Sequenz-Regel** über
den ganzen Strom eines Subjects und gehört darum zur Übergangsschicht.

Das Modell trägt sie als optionale Eigenschaft der Kante (`model.Edge.Cardinality`,
Werte `once` | `many`; leer = unbeschränkt). Die Engine zählt beim Graphlauf,
welche `once`-Typen ein Subject schon gesehen hat, und meldet ein zweites
Auftreten als Abweichung — **auch dort, wo die Topologie es erlauben würde** (ein
Self-Loop, ein Rückkehr-Pfad zum Startzustand). Bei sauberer Topologie ist `once`
für reine Anlage-Events redundant (aus dem Folgezustand führt ohnehin keine
`new`-Kante zurück); seinen eigentlichen Wert spielt es aus, sobald derselbe Typ
strukturell wiederholbar wäre, aber fachlich nur einmal gelten darf, sowie als
**explizite, exportierbare Dokumentation** der Absicht.

Weil die Kardinalität das Ergebnis eines Laufs verändert, fließt sie in den
`DraftRev`-Fingerabdruck ein (Drift-Erkennung) und der Generator (§4) hält sie
ein: eine `once`-Kante wird je Strom höchstens einmal begangen, damit erzeugte
Ströme nie der Regel widersprechen, die die Engine prüft. Beispiel:
`examples/teststudio/draft-employee-identity.json` mit zugehöriger Suite.

### 6.4 Invarianten / Preconditions

Das Modell sieht optionale Preconditions/Invarianten pro Kante vor
(`WORKBENCH.md` §4.3, §5.4). Clio selbst spricht **CEL** für Abfragen — naheliegend
ist, Invarianten als CEL-Ausdrücke über `data` (und ggf. akkumuliertem
Zustand) zu formulieren, damit dieselbe Sprache wie in Clio gilt. Das ist die
ausbaufähigste Schicht und für v1 optional; v1 darf sich auf Schema + Übergang
beschränken.

> Bewusste Einschränkung: Die lokale Engine prüft *gegen den Entwurf*. Sie ist
> nicht Clios Server. Wo es darum geht, ob **Clio** eine Payload/Registrierung
> wirklich akzeptiert (z.B. Schema-Unveränderlichkeit, serverseitige Regeln),
> führt kein Weg an einem echten Push gegen eine Instanz vorbei — siehe §7.

---

## 7. Integration mit einer Clio-Instanz

Lokal lässt sich viel prüfen; manches braucht den echten Server. Das Studio
nutzt dafür den **bestehenden** `/api`-Reverse-Proxy (Token serverseitig,
`FlushInterval: -1`).

### 7.1 Betanken (Seed / Fixtures)

Generierte Ströme (Abschnitt 4) als Events in eine Instanz schreiben:
Demodaten, Fixtures, Last. Anschließend lassen sich die eigenen
Analyse-Panels und die Gegenprobe an realistischem, *bekanntem* Material
erproben.

### 7.2 Round-Trip-Prüfung

Erzeugen → pushen → über Read-Queries zurücklesen → gegen die Erwartung des
Szenarios halten. Das prüft den **vollen** Pfad inklusive Clios eigener
serverseitiger Annahme/Ablehnung — die einzige Schicht, die lokal prinzipiell
nicht abbildbar ist.

### 7.3 Die append-only-Falle (ehrliche Einordnung)

Clio ist **append-only** und Schemas sind **unveränderlich**. Daraus folgt
hart:

- **Testdaten lassen sich nicht löschen.** Wer eine produktive Instanz betankt,
  verschmutzt sie dauerhaft.
- **Schema-Tests verbrennen Schemas.** Ein einmal registriertes (Test-)Schema
  bleibt; ein zweiter, abweichender Testlauf kollidiert.

Konsequenz und Empfehlung: Round-Trip- und Push-Tests laufen **nur gegen eine
Wegwerf-Instanz** (eigener Container, eigene Datenablage) oder mindestens
gegen einen **klar isolierten, eindeutigen Subject-Namensraum** pro Lauf
(z.B. `/_test/{run-id}/orders/{id}`). Das Studio muss diese Trennung sichtbar
erzwingen — etwa indem es ohne markierten Test-Scope **keinen** Push erlaubt
und im Header (Space-Look-HUD) unübersehbar warnt, gegen welchen Server gerade
geschrieben würde. Das ist dieselbe Sorgfalt, mit der die Workbench heute schon
den Event-Cap und die Verbindung anzeigt.

---

## 8. Reproduzierbarkeit & Reporting

Ein Lauf produziert einen **Report** als Artefakt:

- Kopf: Modell-ID + Rev, Suite, **Seed**, Zeitpunkt, Ziel (lokal / Instanz-URL).
- Pro Case: grün/rot, der durchlaufene Pfad, bei Rot die *erste* abweichende
  Stelle samt Grund (Schemafehler-Feld, fehlende Kante, verletzte Invariante).
- Zusammenfassung: Bestanden/Fehlgeschlagen, **Kanten-Überdeckung** des
  Graphen (welche Event-Typen/Übergänge ein Lauf je berührt hat).

Formate konsistent mit dem bestehenden Export (`WORKBENCH.md` §6.2): **Markdown**
fürs Repo/Wiki und **JSON** zum Weiterverarbeiten. Weil alles über den Seed
deterministisch ist, ist ein Report exakt wiederholbar — und damit als
Regressionsnachweis tauglich.

---

## 9. Producer-Code in mehreren Sprachen & Plattformen

Ein bestandener Test beweist, dass das *Modell* trägt. Den **Producer** — den
Code, der die Events tatsächlich an Clio anhängt — muss aber noch jemand
schreiben. Das Studio bietet ihn als generiertes **Beispiel/Gerüst** an: aus
demselben Modell, gegen das die Szenarien prüfen, entsteht Producer-Code in
mehreren Sprachen und für mehrere Plattformen — **konform per Konstruktion**.

Das schließt die Schleife *Entwurf → Bauen → Testen*: Weil Producer-Code und
Tests aus derselben Quelle stammen, emittiert der generierte Producer genau die
Events, die die Szenarien validieren — und das Studio kann es beweisen
(Abschnitt 9.3).

### 9.1 Was generiert wird

Pro Event-Typ erzeugt das Studio aus dem Modell:

- einen **typisierten** Payload-Träger (Struct/Klasse/Interface) aus dem
  `data`-JSON-Schema des Event-Typs;
- einen **Aufruf** gegen Clios öffentlichen Append-/Create-Endpunkt, der das
  CloudEvents-Envelope korrekt füllt (`type`, `subject`, `data`, ggf.
  `source`/`id`);
- einen **Subject-Helfer** aus dem Subject-Stil des Modells (`/orders/{id}`),
  damit Subjects nicht von Hand zusammengestückelt werden;
- **Auth** über Bearer-Token, das aus Env/Config gelesen wird — **nie**
  hartkodiert im Snippet.

Optional füllt der Schema-Faker (Abschnitt 4.2) die Beispielaufrufe mit
schema-gültigen Beispieldaten, sodass das Snippet **sofort lauffähig** gegen
eine Clio ist, nicht nur ein Gerippe.

### 9.2 Sprachen & Plattformen

Je Sprache ein eigenes, eingebettetes `text/template` — kein Build-Step, kein
CDN, alles im Binary:

| Sprache / Plattform | Variante |
|---|---|
| Go | `net/http` |
| TypeScript / JavaScript | Node (`fetch`/`undici`) **und** Browser-`fetch` |
| Python | `requests` / `httpx` |
| Java | `java.net.http.HttpClient` |
| C# / .NET | `HttpClient` |
| Rust | `reqwest` |
| `curl` / Shell | kleinster gemeinsamer Nenner; taugt zugleich als Smoke-Test |
| CloudEvents-SDK | wo ein offizielles SDK existiert (Clio ist CloudEvents-basiert) |

Die Liste ist eine Empfehlung, kein Versprechen — welche Sprachen v1 ausliefert,
ist eine offene Frage (Abschnitt 12).

### 9.3 Designentscheidungen & ehrliche Einordnung

- **Generiert aus dem Entwurf**, wie Schemas und Doku (`WORKBENCH.md` §6). Der
  Entwurf bleibt die Quelle der Wahrheit; der Code wird daraus *erzeugt*.
- **Gerüst, kein SDK.** Es sind **Beispiele/Startpunkte** zum Kopieren — kein
  versioniertes, gepflegtes SDK. Sie zeigen die *Form*; ab dem Kopieren gehört
  der Code der Entwicklerin. Das ehrlich zu benennen verhindert die falsche
  Erwartung einer mitwachsenden Client-Bibliothek.
- **Nur die öffentliche API.** Die Templates rufen ausschließlich dokumentierte
  HTTP-Endpunkte; das Token kommt aus der Umgebung des *Producers*, nie inline.
- **Beweisbar korrekt, nicht nur plausibel.** Das Studio kann einen generierten
  Beispielaufruf gegen eine **Wegwerf-Instanz** (Abschnitt 7) absetzen und das
  angehängte Event mit der Engine (Abschnitt 6) zurückprüfen — so ist belegt,
  dass das Beispiel wirklich konforme Events erzeugt.

### 9.4 Abgrenzung — Studio oder Workbench-Export?

Producer-Code ist streng genommen ein **Generierungs-Artefakt** wie der
Schema-/Doku-Export (`WORKBENCH.md` §6) und könnte ebenso dort sitzen. Er steht
hier, weil seine Daseinsberechtigung das *Testen* ist: Der Producer ist das
Gegenstück, das die Szenarien validieren, und entsteht aus derselben Quelle. Wo
das Feature am Ende in der UI hängt (Studio-Tab vs. Export-Dialog), ist eine
Detailfrage; die Generierungslogik (`internal/producergen`) ist davon
unabhängig.

---

## 10. UI & Einbettung in die Shell

Das Studio fügt sich in die VS-Code-Schale ein (`FRAMEWORK.md`) — kein
Sonderweg:

- **Eigene Activity „Teststudio"** in der Activity-Bar.
- **Sidebar**: Suites & Szenarien (anlegen, auswählen, Seed setzen), Test-Scope
  (Wegwerf-Ziel, Subject-Präfix).
- **Editor-Tabs**: *Generator* (Strom erzeugen & vorschauen), *Szenario-Editor*,
  *Pfad-Ansicht* (durchlaufener Pfad im Graphen, Space-Look), *Producer-Code*
  (Sprachumschalter, Snippet pro Event-Typ, „kopieren"/„herunterladen").
- **Panel unten**: *Testlauf* (Ergebnisliste, rot/grün) neben dem bestehenden
  *Output*/*Konformität* — fachlich derselbe Ort wie die Gegenprobe.

Eine neue Ansicht ist nach `FRAMEWORK.md` je ein Handler + ein Body-Template in
`views.html` + ein `View`-Eintrag in `contributions()`. Keine
Framework-Änderung nötig.

### 10.1 Vorgeschlagene Go-Pakete

Konsistent mit der bestehenden Paketnamensgebung (`schemagen`, `bpmngen`,
`process`, `store`, …):

| Paket | Aufgabe |
|---|---|
| `internal/scenario` | Datenmodell + Store für Suites/Cases (analog `store`/`envstore`) |
| `internal/simulator` | Graph-Walk + Payload-Faker + Mutation (Abschnitt 4) |
| `internal/validate` | Schema-, Übergangs-, Invarianten-Engine (Abschnitt 6); **geteilt** mit der Gegenprobe |
| `internal/producergen` | Producer-Code je Sprache/Plattform aus dem Modell (Abschnitt 9) |
| `internal/testreport` | Report-Rendering Markdown/JSON (Abschnitt 8) |
| `internal/server` | Handler + Shell-Beiträge (Abschnitt 10) |

`internal/validate` ist bewusst das gemeinsame Herz: Die Gegenprobe
(`conformance.go`) soll künftig dieselbe Übergangs-/Schema-Prüfung nutzen, statt
eine zweite Implementierung zu pflegen.

---

## 11. Roadmap (Clio-Stufenlogik)

Greift in die Workbench-Stufen (`WORKBENCH.md` §8): das Studio setzt sinnvoll
nach **Stufe 2** (Event-Typen & Schemas existieren) auf.

- **Stufe T0 — Engine & Schema-Tests.** `internal/validate` mit Schema- und
  Übergangsprüfung; Schema-Test-Ansicht (3.1). Rein lokal, keine Instanz.
- **Stufe T1 — Szenarien.** `internal/scenario`-Datenmodell + Store,
  Szenario-Editor, Sequenz-Tests (3.2/3.3), Pfad-Ansicht im Graphen.
- **Stufe T2 — Generator.** Graph-Walk + Schema-Faker + Mutation (Abschnitt 4),
  property-based Stichproben, Report (Abschnitt 8).
- **Stufe T3 — Producer-Code.** `internal/producergen`: Beispiel-Producer je
  Sprache/Plattform aus dem Modell (Abschnitt 9), mit dem Schema-Faker gefüllt
  und über die Engine als konform belegt.
- **Stufe T4 — Instanz-Integration.** Betanken/Fixtures und Round-Trip gegen
  eine **Wegwerf-Instanz** mit erzwungenem Test-Scope (Abschnitt 7).
- **Stufe T5 — Konsolidierung.** Die Gegenprobe (`WORKBENCH.md` §7) auf
  `internal/validate` umstellen, sodass Soll-Tests und Ist-Prüfung denselben
  Code teilen.

---

## 12. Offene Fragen

1. **JSON-Schema-Bibliothek.** Welche reine-Go-Bibliothek („embedbar, kein CDN,
   kompatible Lizenz") für die Validierung — und deckt sie den Schema-Dialekt
   ab, den der Workbench-Export erzeugt?
2. **Invarianten-Sprache.** CEL (wie Clio) für maximale Nähe, oder zunächst gar
   keine Invarianten und nur Schema + Übergang? CEL zieht eine echte
   Abhängigkeit nach sich — Preis gegen Konsistenz abwägen.
3. **Akkumulierter Zustand.** Reicht für Sequenz-Tests der reine Graph-Zustand
   (Knoten), oder braucht es einen aus den Payloads gefalteten Aggregatzustand,
   damit Invarianten über *Werte* (z.B. „Betrag > 0") prüfbar werden?
4. **Test-Scope-Garantie.** Wie hart erzwingt das Studio die Trennung von
   produktiven Instanzen (Abschnitt 7.3)? Nur Warnung, oder Push grundsätzlich
   nur bei explizit als „Wegwerf" markiertem Ziel?
5. **Szenario-Herkunft.** Szenarien von Hand schreiben, *oder* aus der
   Gegenprobe ernten — ein real beobachteter, überraschender Pfad wird per Klick
   zum Regressions-Szenario („so soll/soll-nicht es laufen")?
6. **Determinismus über Versionen.** Bleibt ein Seed stabil, wenn sich die
   Generator-Logik ändert? Vermutlich nein — wie wird das im Report kenntlich
   gemacht, damit ein „grün→rot" nicht fälschlich als Regression gilt?
7. **Producer-Sprachen in v1.** Welche der Sprachen/Plattformen aus Abschnitt 9.2
   liefert v1 aus — und nach welchem Kriterium (Verbreitung in der Clio-Nutzung,
   Wartbarkeit der Templates)? Lieber wenige, gepflegte Beispiele als viele
   halbgare.
8. **Producer-Code: Studio oder Export?** Hängt der Producer-Code am Studio-Tab
   oder am Workbench-Export-Dialog (`WORKBENCH.md` §6)? Die Logik
   (`internal/producergen`) ist davon unabhängig — es ist eine reine
   UI-/Einordnungsfrage (Abschnitt 9.4).
