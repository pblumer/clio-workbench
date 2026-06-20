Lernpfad — Test Studio
======================

Ein geführter, praktischer Einstieg ins **Test Studio** der Clio Workbench.
Du arbeitest dich vom einfachsten Baustein (eine Payload prüfen) bis zum vollen
Kreislauf (gegen eine Instanz pushen und das Soll mit dem Ist vergleichen) vor.

Der Pfad nutzt durchgehend das mitgelieferte Demo-Modell **Order Lifecycle**
(`draft-order-lifecycle.json`) und die Demo-Suite
(`suite-order-lifecycle-tests.json`) aus diesem Verzeichnis. Jede Lektion baut
auf der vorigen auf und folgt den vier Prüfarten und Stufen aus
[`../../docs/TESTSTUDIO.md`](../../docs/TESTSTUDIO.md).

> **Konzeptueller Hintergrund:** Das *Warum* hinter jeder Lektion steht im
> Ideenpapier [`TESTSTUDIO.md`](../../docs/TESTSTUDIO.md). Dieser Pfad ist das
> *Wie* — die Tastatur dazu. Jede Lektion verweist auf den passenden Abschnitt.

Überblick
---------

| # | Lektion | Prüfart / Stufe | Du brauchst eine Instanz? | Zeit |
|---|---------|-----------------|---------------------------|------|
| 0 | Aufsetzen & importieren        | —                     | nein            | 5 min |
| 1 | Schema-Test: passt die Payload?| Schema (§3.1, T0)     | nein            | 10 min |
| 2 | Sequenz-Test: stimmt der Pfad? | Übergang (§3.2, T0/T1)| nein            | 15 min |
| 3 | Szenarien & Suites             | Szenario (§3.3, T1)   | nein            | 15 min |
| 4 | Generator & Report             | Generator (§3.4, T2)  | nein            | 15 min |
| 5 | Producer-Code erzeugen         | Producer (§9, T3)     | nein            | 10 min |
| 6 | Push & Round-Trip              | Instanz (§7, T4)      | **ja** (Wegwerf)| 20 min |
| 7 | Gegenprobe: Soll vs. Ist       | Konsolidierung (T5)   | **ja** (Wegwerf)| 15 min |

Die Lektionen 0–5 laufen **vollständig offline** am Entwurf — keine Clio-Instanz
nötig. Erst 6 und 7 brauchen einen Server, und zwar bewusst nur eine
**Wegwerf-Instanz** (siehe Lektion 6).

Voraussetzungen
---------------

- Go installiert (für `go run ./cmd/clio-workbench`), oder ein gebautes Binary.
- Dieses Repository ausgecheckt.
- Für Lektion 6/7 zusätzlich eine **Wegwerf-Clio** (eigener Container, eigene
  Datenablage) — *niemals* eine produktive Instanz, siehe Lektion 6.

---

Lektion 0 — Aufsetzen & importieren
-----------------------------------

**Ziel:** Workbench starten und das Demo-Modell samt Suite ins Studio bekommen.

1. Wähle ein Datenverzeichnis und kopiere die Artefakte hinein. Der Draft kommt
   in das Datenverzeichnis selbst, die Suite in das `scenarios/`-Unterverzeichnis
   (damit sie nicht für einen Draft gehalten wird):

   ```sh
   export WORKBENCH_DATA=./workbench-data
   mkdir -p "$WORKBENCH_DATA/scenarios"
   cp examples/teststudio/draft-order-lifecycle.json        "$WORKBENCH_DATA/"
   cp examples/teststudio/suite-order-lifecycle-tests.json  "$WORKBENCH_DATA/scenarios/order-lifecycle-tests.json"
   ```

2. Starte die Workbench (offline genügt für die ersten Lektionen):

   ```sh
   go run ./cmd/clio-workbench
   # öffne http://localhost:8080
   ```

3. Öffne in der **Activity-Bar** links die Activity **„Teststudio"**.

**Erwartetes Ergebnis:** Das Studio öffnet sich, und in der Modell-Auswahl
erscheint **Order Lifecycle**. In der Szenarien-Sidebar erscheint die Suite
**„Order Lifecycle – Demo-Testsuite"**.

> **Tipp:** Du kannst Draft und Suite auch über *Import von URL* laden, falls
> deine Workbench das anbietet — das Ergebnis ist dasselbe.

**Vertiefung:** Das Modell selbst (Knoten, Kanten, Event-Typ-Felder) ist im
[`README.md`](README.md) dieses Verzeichnisses kompakt beschrieben. Wirf einen
Blick darauf, bevor es weitergeht — es ist die Messlatte für alles Folgende.

---

Lektion 1 — Schema-Test: passt die Payload?
-------------------------------------------

**Prüfart:** Schema-Tests (`TESTSTUDIO.md` §3.1) · **Stufe:** T0

**Ziel:** Verstehen, wie eine einzelne `data`-Payload gegen das Schema **eines**
Event-Typs geprüft wird — die kleinste, lokalste Prüfung.

**Was du lernst:** Pflichtfelder, Typen und Formate. Warum ein roter Test
*feldgenau zeigt, warum* er rot ist.

**Schritte:**

1. Öffne den Editor-Tab **Schema-Test**. Wähle Modell **Order Lifecycle** und
   Event-Typ **`order-placed`**. Du siehst die Schema-Vorschau: `customerEmail`
   (string, required, format email) und `itemsCount` (integer, required).

2. **Grüner Fall** — gib eine gültige Payload ein und prüfe:

   ```json
   { "customerEmail": "alice@example.com", "itemsCount": 3 }
   ```

   → **accept**, alle Felder grün.

3. **Pflichtfeld fehlt** — entferne `customerEmail`:

   ```json
   { "itemsCount": 3 }
   ```

   → **reject**. Der Fehler nennt das Feld `customerEmail` und die Regel
   *required*.

4. **Typ-Fehler** — `itemsCount` als String:

   ```json
   { "customerEmail": "alice@example.com", "itemsCount": "drei" }
   ```

   → **reject**, Feld `itemsCount`, Regel *type* (integer erwartet).

5. **Format-Fehler** — kaputte E-Mail:

   ```json
   { "customerEmail": "keine-email", "itemsCount": 3 }
   ```

   → **reject**, Feld `customerEmail`, Regel *format* (email).

**Übung:** Wechsle auf `order-shipped` und provoziere einen **enum**-Fehler beim
Feld `carrier` (erlaubt: `DHL`, `UPS`, `FedEx`). Erzeuge danach eine **gültige**
`order-shipped`-Payload mit korrekter UUID in `trackingId`.

**Selbstcheck:** Du kannst für jeden Event-Typ benennen, *welches* Feld und
*welche* Regel einen Fehler auslöst — nicht nur „invalid".

---

Lektion 2 — Sequenz-Test: stimmt der Pfad?
------------------------------------------

**Prüfart:** Übergangs- & Sequenz-Tests (`TESTSTUDIO.md` §3.2) · **Stufe:** T0/T1

**Ziel:** Nicht mehr eine einzelne Payload, sondern eine **Folge** von
Event-Typen gegen den Lifecycle-Graphen halten.

**Was du lernst:** Eine Sequenz muss an einem **Startzustand** beginnen, jeder
Schritt muss eine **existierende Kante** sein, und das Ergebnis ist der
**durchlaufene Pfad** — nicht nur grün/rot.

Erinnerung an den Graphen (aus dem Modell):

```
cart ──order-placed──▶ placed ──order-paid──▶ paid ──order-shipped──▶ shipped ──order-delivered──▶ completed
  │                       │
  └──order-cancelled──────┴──order-cancelled──▶ cancelled
```

**Schritte:** Lege im Tab **Szenarien** ein neues, leeres Szenario an (oder nutze
das Sequenzfeld) und gib die Event-Typ-Folge als `→`- oder Komma-Liste ein.

1. **Gültiger Vollpfad:**
   `order-placed → order-paid → order-shipped → order-delivered`
   → **accept**, Endzustand **completed**. Der Pfad leuchtet
   `cart→placed→paid→shipped→completed`.

2. **Gültige Stornierung:**
   `order-placed → order-cancelled`
   → **accept**, Endzustand **cancelled**.

3. **Ungültige Reihenfolge** (Versand ohne Bezahlung):
   `order-placed → order-shipped`
   → **reject**. Die erste Abweichung ist Schritt 2: von `placed` gibt es **keine
   Kante** `order-shipped`. Der Pfad endet sichtbar bei `placed`.

4. **Falscher Start:** `order-paid → …`
   → **reject**: `order-paid` ist kein Übergang aus einem Startzustand.

**Übung:** Finde die im Modell verbotene Storno-Variante: Kann man **nach**
Versand noch stornieren? Probiere
`order-placed → order-paid → order-shipped → order-cancelled` und erkläre die
Ablehnung (es gibt keine Kante `order-cancelled` aus `shipped`).

**Selbstcheck:** Du kannst zu jeder Ablehnung sagen, *an welchem Schritt* und
*aus welchem Zustand* die Sequenz ausbricht.

---

Lektion 3 — Szenarien & Suites
------------------------------

**Prüfart:** Szenario-Tests (`TESTSTUDIO.md` §3.3) · **Stufe:** T1

**Ziel:** Aus losen Sequenzen werden **benannte, versionierbare** Fälle mit einer
**Erwartung** — das eigentliche Artefakt des Studios, das man git-t.

**Was du lernst:** Aufbau einer Suite (`Case`/`Step`/`Expectation`), der
Unterschied der Erwartungen `accept` / `reject` / `endState`, und die
**Drift-Erkennung** über `draftRev`.

**Schritte:**

1. Wähle in der Sidebar die Suite **„Order Lifecycle – Demo-Testsuite"** und
   klicke **„Alle prüfen"**. Du siehst fünf Fälle:

   | Fall | Erwartung | Warum |
   |------|-----------|-------|
   | `happy-path-standard`               | accept, endState `completed` | gültiger Vollpfad mit allen Payloads |
   | `cancel-before-payment`             | accept, endState `cancelled` | gültige Stornierung aus `placed` |
   | `invalid-sequence-ship-without-pay` | reject | `order-shipped` hat keine Kante aus `placed` |
   | `payload-missing-required-field`    | reject | `customerEmail` fehlt |
   | `payload-type-mismatch`             | reject | `itemsCount` ist String statt Integer |

   → Alle fünf sollten **bestehen** (die „reject"-Fälle bestehen, *weil* sie
   korrekt abgelehnt werden — die Erwartung trifft ein).

2. Sieh dir die Quelle [`suite-order-lifecycle-tests.json`](suite-order-lifecycle-tests.json)
   an. Beachte: `draftId` referenziert das Modell, `draftRev` (`c6ac54f6d18d`)
   hält fest, *gegen welchen Stand* die Suite geschrieben wurde. Die Suite
   **kopiert** das Modell nicht — sie verweist darauf (Leitprinzip §5).

3. **Drift erleben (optional):** Ändere am Modell etwas Test-Relevantes (z.B.
   benenne den Event-Typ `order-paid` um) und speichere. Das Studio zeigt eine
   **Drift-Warnung**: Die Suite wurde gegen einen anderen Stand geschrieben und
   sollte neu geprüft werden. Mach die Änderung danach rückgängig.

**Übung:** Füge der Suite einen eigenen Fall hinzu, der genau den verbotenen
Storno-nach-Versand aus Lektion 2 als **reject** festschreibt:

```json
{
  "id": "cancel-after-ship-forbidden",
  "name": "Storno nach Versand ist verboten",
  "seed": 47,
  "steps": [
    { "type": "order-placed",  "subject": "/orders/ORD-2025-006",
      "data": { "customerEmail": "erin@example.com", "itemsCount": 1 } },
    { "type": "order-paid",     "data": { "amount": 10.0, "paidAt": "2025-06-20T10:00:00Z" } },
    { "type": "order-shipped",  "data": { "trackingId": "770e8400-e29b-41d4-a716-446655440002", "carrier": "FedEx" } },
    { "type": "order-cancelled","data": { "reason": "zu spät" } }
  ],
  "expect": { "outcome": "reject" }
}
```

Lass die Suite erneut laufen — dein neuer Fall sollte **bestehen** (korrekt
abgelehnt).

**Selbstcheck:** Du verstehst, warum ein „reject"-Szenario ein *bestandener*
Test ist, und wozu `draftRev` dient.

---

Lektion 4 — Generator & Report
------------------------------

**Prüfart:** Generator / Simulator (`TESTSTUDIO.md` §3.4, §4) · **Stufe:** T2

**Ziel:** Statt Fälle von Hand zu schreiben, den Graphen **selbst ablaufen**
lassen und schema-konforme Ströme erzeugen — reproduzierbar über einen **Seed**.

**Was du lernst:** Determinismus (gleicher Seed ⇒ gleicher Strom), property-based
Stichproben („N zufällige Pfade, alle grün"), **Kanten-Überdeckung** und der
**Report** als Artefakt (Markdown/JSON).

**Schritte:**

1. Öffne den Tab **Generator**. Setze einen **Seed** (z.B. `42`) und eine
   **Stichprobenzahl** (z.B. `50`). Klicke **„Generieren & prüfen"**.

2. Lies das Ergebnis:
   - **N/N gültig** — jeder erzeugte Strom besteht die Engine aus Lektion 1+2.
   - **Kanten-Überdeckung** — welche Übergänge des Graphen mindestens einmal
     vorkamen. (Seltene Kanten wie `order-cancelled` brauchen evtl. mehr Stichproben
     oder einen anderen Seed.)
   - **Negativ-Prüfungen** — gezielt **mutierte** Ströme (Pflichtfeld weg, Typ
     verfälscht, Nicht-Kante eingeschoben, Reihenfolge getauscht) müssen
     **abgelehnt** werden. Das beweist: die Prüfung *lehnt auch ab*, winkt nicht
     nur durch.

3. **Determinismus prüfen:** Laufe denselben Seed zweimal — Ergebnis und Report
   sind Bit für Bit identisch. Ändere den Seed — der Strom ändert sich.

4. **Report-Download:** Lade den Report als **Markdown** (fürs Repo/Wiki) und als
   **JSON** (zum Weiterverarbeiten) herunter. Der Kopf hält Modell + Rev, Seed und
   Zeitpunkt fest — damit ist der Lauf ein **Regressionsnachweis**.

**Übung:** Finde einen Seed (oder erhöhe die Stichprobenzahl), bei dem die
Kanten-Überdeckung **100 %** erreicht — also auch beide `order-cancelled`-Kanten
mindestens einmal durchlaufen werden.

**Selbstcheck:** Du kannst erklären, warum ein Seed im Report festgehalten wird
und was die Negativ-Prüfungen beweisen.

---

Lektion 5 — Producer-Code erzeugen
----------------------------------

**Prüfart:** Producer-Code (`TESTSTUDIO.md` §9) · **Stufe:** T3

**Ziel:** Aus demselben Modell **Beispiel-Producer-Code** erzeugen — den Code,
der die Events später wirklich an Clio anhängt.

**Was du lernst:** Producer-Code entsteht *aus dem Entwurf* (wie Schemas und
Doku) und ist **Gerüst, kein SDK** — ein Startpunkt zum Kopieren, kein
mitwachsendes Client-Paket.

**Schritte:**

1. Öffne den Tab **Producer-Code**. Wähle Modell **Order Lifecycle**.

2. Schalte zwischen den Sprachen um — v1 liefert **Go**, **TypeScript (fetch)**,
   **Python** und **curl**. Pro Event-Typ siehst du:
   - einen **typisierten Payload-Träger** aus dem `data`-Schema,
   - einen **CloudEvents-POST** an Clios öffentlichen Append-Endpunkt
     (`/api/v1/events`),
   - einen **Subject-Helfer** aus dem Subject-Stil `/orders/{id}`,
   - **Auth** über Bearer-Token aus der **Umgebung** (`CLIO_*`), nie hartkodiert.

3. **Kopieren / Herunterladen** des Snippets deiner Lieblingssprache.

**Übung:** Vergleiche die Go- und die curl-Variante für `order-placed`. Beide
füllen dasselbe CloudEvents-Envelope (`type`, `subject`, `data`) — die curl-Form
taugt nebenbei als kleinster Smoke-Test gegen eine laufende Clio.

**Selbstcheck:** Du kannst benennen, woher der Token im generierten Code kommt
(Umgebung, nicht inline) und warum der Code „Gerüst, kein SDK" ist.

---

Lektion 6 — Push & Round-Trip *(braucht eine Wegwerf-Instanz)*
--------------------------------------------------------------

**Prüfart:** Instanz-Integration (`TESTSTUDIO.md` §7) · **Stufe:** T4

> ⚠️ **Die append-only-Falle — bitte zuerst lesen.** Clio ist **append-only**
> und Schemas sind **unveränderlich**. Geschriebene Testdaten lassen sich
> **nicht löschen**, und ein registriertes Test-Schema **bleibt**. Pushe daher
> **nur gegen eine Wegwerf-Instanz** (eigener Container, eigene Datenablage) —
> *niemals* gegen eine produktive Clio. Das Studio erzwingt das mit einem
> harten Gate (siehe unten); ignoriere die Warnung nicht.

**Ziel:** Den vollen Pfad prüfen — erzeugte Ströme **pushen** und über
Read-Queries **zurücklesen**, inklusive Clios *eigener* serverseitiger
Annahme/Ablehnung (die einzige Schicht, die lokal prinzipiell nicht abbildbar
ist).

**Schritte:**

1. Starte die Workbench gegen deine Wegwerf-Instanz:

   ```sh
   CLIO_URL=http://localhost:3000 CLIO_API_TOKEN=<token> \
     WORKBENCH_DATA=./workbench-data go run ./cmd/clio-workbench
   ```

   Das Verbindungs-Pill im Header sollte **UPLINK** (grün) zeigen.

2. Öffne den Tab **Push**. Beachte das **Gate**: Push ist gesperrt, bis du die
   aktive Instanz **explizit als Wegwerf bestätigst**. Ein Serverwechsel
   *entwaffnet* das Gate automatisch wieder.

3. Bestätige die Wegwerf-Instanz und pushe einen generierten Strom. Das Studio
   präfixt **alle** Subjects automatisch unter `/_test/<run-id>/…`, damit der
   Testlauf einen klar isolierten Namensraum belegt.

4. **Round-Trip:** Das Studio liest die gepushten Events zurück, gruppiert sie
   pro Subject und prüft jede Sequenz erneut mit der Engine aus Lektion 2.

**Erwartetes Ergebnis:** Push akzeptiert, Round-Trip grün. Ohne
Wegwerf-Bestätigung **verweigert** das Studio den Push — das ist beabsichtigt.

**Selbstcheck:** Du kannst erklären, warum Push-Tests nur gegen Wegwerf-Instanzen
laufen dürfen und was das `/_test/<run-id>/`-Präfix bezweckt.

---

Lektion 7 — Gegenprobe: Soll vs. Ist *(braucht eine Wegwerf-Instanz)*
---------------------------------------------------------------------

**Prüfart:** Konsolidierung / Gegenprobe (`TESTSTUDIO.md` §6, T5) · **Stufe:** T5

**Ziel:** Den Bogen schließen — den **Entwurf (Soll)** gegen die **realen Events
(Ist)** einer Instanz halten, mit *derselben* Engine, die in Lektion 1–6 das Soll
geprüft hat.

**Was du lernst:** Soll- und Ist-Seite teilen `internal/validate`. Die Gegenprobe
beantwortet drei Fragen: Gibt es **abweichende Subjects** (Sequenzen, die der
Entwurf nicht erlaubt)? Ist der Entwurf **tot** (Kanten, die real nie vorkommen)?
Gibt es **unbekannte Typen** (Events, die der Entwurf nicht kennt)?

**Schritte:**

1. Sorge dafür, dass deine Wegwerf-Instanz Events unter dem Subject-Präfix des
   Modells (`/orders/…` bzw. das `/_test/…`-Präfix aus Lektion 6) enthält — etwa
   aus dem Push der vorigen Lektion.

2. Öffne die **Gegenprobe** (Soll/Ist) und lass sie gegen die aktive Instanz
   laufen. Sie liest die realen, auf das Präfix gescopten Events, gruppiert pro
   Subject und prüft jede Sequenz mit `validate.CheckSequence`.

3. Lies die drei Befunde: abweichende Subjects, tote Entwurfsteile, unbekannte
   Typen.

**Übung:** Pushe in Lektion 6 *bewusst* einen **mutierten** (roten) Strom und
beobachte, wie die Gegenprobe das betroffene Subject als Abweichung vom Soll
markiert.

**Selbstcheck:** Du kannst erklären, warum Soll-Tests (Szenarien/Generator) und
Ist-Prüfung (Gegenprobe) dieselbe Engine nutzen — und was das für die
Verlässlichkeit beider bedeutet.

---

Geschafft — was du jetzt kannst
-------------------------------

- Eine **Payload** feldgenau gegen ein Schema prüfen (§3.1).
- Eine **Sequenz** gegen den Lifecycle-Graphen halten und Abweichungen verorten
  (§3.2).
- **Szenarien** und **Suites** als versionierbare Artefakte schreiben und Drift
  erkennen (§3.3, §5).
- Mit dem **Generator** reproduzierbare Ströme erzeugen, Kanten-Überdeckung
  messen und einen **Report** als Regressionsnachweis ablegen (§4, §8).
- **Producer-Code** aus dem Modell erzeugen und einordnen (§9).
- Gegen eine **Wegwerf-Instanz** pushen, einen **Round-Trip** fahren und die
  **append-only-Falle** vermeiden (§7).
- Den Entwurf per **Gegenprobe** gegen reale Events halten — Soll und Ist auf
  derselben Engine (§6, T5).

Weiterführend
-------------

- [`TESTSTUDIO.md`](../../docs/TESTSTUDIO.md) — Architektur- & Ideenpapier (das
  *Warum*).
- [`TESTSTUDIO-IMPLEMENTATION.md`](../../docs/TESTSTUDIO-IMPLEMENTATION.md) — die
  Arbeitspakete (WP-1…WP-9) und was jedes Paket abnahmefähig liefert.
- [`README.md`](README.md) — kompakte Beschreibung des Demo-Modells und der Suite.
- [`FRAMEWORK.md`](../../docs/FRAMEWORK.md) — wie das Studio sich in die
  VS-Code-Schale einfügt.
</content>
</invoke>
