# Clio Workbench — KI-Unterstützung · Architektur- & Ideenpapier

**Status:** `KONZEPT` · **Version:** 0.1 · **Bezug:** [`WORKBENCH.md`](WORKBENCH.md) §2, §4, §6 · [`FRAMEWORK.md`](FRAMEWORK.md)

> Ein mitdenkender Assistent am Zeichenbrett. Die Workbench soll einen
> Sprachmodell-Dienst um Hilfe bitten können — allen voran beim *Erzeugen* eines
> Entwurfs aus einer Beschreibung in natürlicher Sprache —, ohne dafür eines
> ihrer sechs Leitprinzipien aufzugeben. Dieses Papier klärt, *wie* das geht:
> wo der Key liegt, wem man dabei vertrauen muss, und wie ein KI-„Auftrag"
> aussieht.

---

## 1. Motivation & Abgrenzung

Das Herzstück der Workbench ist der Entwurf: ein gerichteter Graph aus Zuständen
und Event-Typen, aus dem Schemas, Producer-Code, BPMN und Doku *generiert*
werden (`WORKBENCH.md` §5, §6). Den Graphen zu zeichnen ist Handarbeit — und
genau hier kann ein Sprachmodell viel abnehmen: aus „ein Bestellprozess mit
Bezahlung, Versand und Storno" einen ersten `Draft` vorschlagen, ein fehlendes
`data`-Schema aus dem Event-Namen erraten, eine Gegenprobe-Abweichung in Prosa
erklären.

Die Leitfrage dieses Papiers ist daher **nicht** „welche KI-Features wären cool",
sondern: **Wie lässt sich ein externer KI-Dienst anbinden, ohne die Identität
der Workbench zu beschädigen?** Die Workbench ist ein lokales, prinzipientreues
Entwicklerwerkzeug — kein KI-Produkt. Die KI ist ein *Werkzeug am Werkzeug*.

### 1.1 Was die KI-Unterstützung ist — und was nicht

- **Sie ist ein BYO-Assistent („bring your own").** Der Key gehört dem Nutzer,
  nicht der Workbench. Die Workbench *besitzt* kein KI-Secret und bietet keinen
  KI-Dienst an, den Dritte mitnutzen — sie reicht im Auftrag des Nutzers eine
  Anfrage durch.
- **Sie ist optional, nie Voraussetzung.** Ohne Key läuft das Entwerfen,
  Validieren, Exportieren und die Gegenprobe vollständig weiter. Die KI fügt
  Beschleunigung hinzu, sie ersetzt keine Funktion (Offline-Prinzip,
  `CLAUDE.md` „Funktioniert offline").
- **Ihr Output ist ein Vorschlag, kein Artefakt.** Die Quelle der Wahrheit
  bleibt der vom Menschen kuratierte `Draft` (Prinzip 5). Ein KI-Vorschlag
  durchläuft dieselbe `internal/validate`-Engine wie jeder andere Entwurf, bevor
  er übernommen wird; nichts wird ungeprüft persistiert.
- **Sie ist kein Agent mit Eigenleben.** Kein autonomer Hintergrund-Loop, kein
  Schreibzugriff auf Clio ohne Bestätigung. Ein „Auftrag" ist eine einzelne,
  vom Nutzer ausgelöste Anfrage mit klar umrissenem Ein- und Ausgang (§4).

---

## 2. Treue zu den Leitprinzipien

Die sechs Prinzipien (`WORKBENCH.md` §2) entscheiden auch hier. Die Anbindung
ist nur dann richtig, wenn sie *keines* verletzt:

1. **Ein Binary, reines Go.** Der KI-Client ist `net/http` gegen die HTTP-API
   des Anbieters — **keine** neue Abhängigkeit, kein SDK. JSON über
   `encoding/json`. Damit bleibt `go.mod` schlank (`CLAUDE.md` „Keine neuen
   Abhängigkeiten").
2. **Kein Build-Step im Frontend.** Prompt-Bau, Anbieter-Aufruf und das
   Übersetzen der Antwort in einen `Draft` liegen in **Go**. Das Frontend löst
   einen Auftrag nur per HTMX aus und rendert das Ergebnis-Fragment — kein neues
   JS-Gewicht (Prinzip 2: „Logik so weit wie möglich in Go").
3. **Nur die öffentliche API.** Gilt unverändert für Clio. Der KI-Anbieter ist
   ein *zusätzlicher* externer Dienst, der ausschließlich über seine
   dokumentierte HTTP-API angesprochen wird — kein privilegierter Zugriff.
4. **Das Secret bleibt serverseitig.** Der KI-Key wird wie der Clio-Token vom
   Backend gehalten und nie an Browser-JS oder gerendertes HTML gegeben (§3, §5).
5. **Der Entwurf ist das Artefakt.** Die KI generiert *in* das Datenmodell
   hinein (`internal/model`), nicht daran vorbei. Ihr Vorschlag ist ein `Draft`
   wie jeder andere und wird so behandelt (§1.1).
6. **Space-Look.** Auftrags-Dialog und Ergebnisse fügen sich in die bestehende
   Schale und ihre Designsprache ein (§6).

Der wunde Punkt ist Prinzip 4 — und genau ihm gilt das nächste Kapitel.

---

## 3. Das Vertrauensmodell — der eigentliche Knackpunkt

Ein KI-Key ist ein Secret. Die naheliegende Sorge: *Muss ich dem Server
vertrauen, wenn ich ihm meinen Key aushändige?* Die Antwort hängt **nicht** an
„Client oder Server", sondern an einer vorgelagerten Frage: **Wer betreibt den
Server?**

### 3.1 Zwei Deployment-Fälle

**Fall A — lokales Single-User-Tool (das heute dokumentierte Design).** Die
Workbench läuft als ein Binary auf der Maschine des Entwicklers (`:8080`,
`./workbench-data`, Konfiguration aus der Umgebung). „Der Server" ist *das eigene
Prozess­abbild*. Dem Server den Key zu geben ist dasselbe, wie ihn in eine
Umgebungsvariable zu legen — man vertraut sich selbst. Es wird **kein** Vertrauen
delegiert. Serverseitig ist hier die natürliche, prinzipientreue Wahl.

**Fall B — gehostete/geteilte Instanz (jemand anderes betreibt sie).** Jetzt ist
die Sorge real: Der Betreiber könnte den Key sehen, protokollieren, nutzen, und
die Kosten landen beim Nutzer. Hier hat der Reflex „dann lieber client-seitig"
spürbares Gewicht.

### 3.2 Warum client-seitig den Knoten nicht löst

Der Reflex trägt nur scheinbar. Denn **dieselbe Instanz liefert auch das
JavaScript aus** (Prinzip 1: alles aus `embed.FS`, kein CDN). Ein bösartiger oder
kompromittierter Betreiber kann JS ausliefern, das den Key direkt aus dem Browser
abzieht — auch wenn der Key „offiziell" nie durch den Server läuft. Das Vertrauen
verschwindet nicht, es **verschiebt sich nur**: von „trau dem gespeicherten Key"
zu „trau dem ausgelieferten Code". Bei einem Werkzeug, dessen ganze Identität
„ein Binary, das sein eigenes Frontend embedded" ist, kommt der Code in *beiden*
Fällen vom selben Server.

Hinzu kommt die technische Realität: Direkte Browser-Aufrufe an die großen
Anbieter sind ein Sonderfall — Anthropic verlangt dafür ein explizites
„dangerous-direct-browser-access"-Opt-in mitsamt CORS, OpenAI rät grundsätzlich
ab. Ein client-seitiger Key wäre also der erste Bruch von Prinzip 4 *und* würde
gegen Prinzip 2 arbeiten (die ganze Übersetzungs-Logik müsste ins JS wandern),
ohne das Vertrauensproblem tatsächlich zu lösen.

### 3.3 Was Vertrauen wirklich reduziert

Unabhängig von Client/Server zählen drei Hebel — und alle drei sind serverseitig
genauso erreichbar:

- **Nicht persistieren (Default).** Der Key lebt nur im Speicher (aus Env oder
  Eingabe), wie der Clio-Token. Eine *spätere* Kompromittierung findet keinen
  gespeicherten Key und kein Key in den Logs.
- **Lokal betreiben.** In Fall A ist „der Server" der Nutzer selbst — die
  Vertrauensfrage kollabiert.
- **Die Annahme dokumentieren.** Die Workbench ist als lokales
  Entwicklerwerkzeug konzipiert. „Gehostet, multi-user" ist eine eigene
  Betriebsentscheidung mit eigenen Konsequenzen (Mandantentrennung, Abrechnung,
  ausgelieferter Code) — sie wird *nicht* durch die Platzierung des Keys gelöst
  und liegt außerhalb des Rahmens dieses Papiers.

**Schlussfolgerung.** Die Trust-Frage ist eine **Deployment-Frage**, keine
Client/Server-Frage. Für das dokumentierte lokale Tool kollabiert sie. Der Key
bleibt deshalb **serverseitig und nur im Speicher** — das ist zugleich die
prinzipientreueste und die ehrlichste Wahl. Client-seitig wird als Alternative
bewusst verworfen (§8, Entscheidung 2).

---

## 4. Architektur

```
   Browser
     │  HTMX: Auftrag auslösen, Ergebnis-Fragment rendern (kein Key, kein JS-Aufruf an den Anbieter)
     ▼
┌──────────────────────────────────────────────┐
│  clio-workbench  (:8080)                       │
│                                                │
│  ├─ internal/server   Auftrags-Handler         │
│  │     │  baut Kontext aus Draft + Scope        │
│  │     ▼                                         │
│  ├─ internal/ai       KI-Client (net/http)      │   ──Key (Header)──┐
│  │     │  Provider-Naht, strukturierte Antwort   │                  │
│  │     ▼                                          │                  │
│  ├─ internal/model    Vorschlag → Draft           │                 │
│  ├─ internal/validate Vorschlag prüfen            │                 │
│  └─ internal/store    erst nach Bestätigung       │                 │
└──────────────────────────────────────────────┘                  ▼
                                                          ┌──────────────────┐
                                                          │  KI-Anbieter-API  │
                                                          │  (HTTPS)          │
                                                          └──────────────────┘
```

### 4.1 `internal/ai` — der KI-Client

Analog zu `internal/clio`: ein dünner HTTP-Client gegen die öffentliche API des
Anbieters. Verantwortlichkeiten:

- Hält Ziel (Endpunkt-URL, Modellname) und Key, austauschbar zur Laufzeit,
  mutex-geschützt — exakt das Muster von `clio.Client.SetTarget`/`Snapshot`.
- `Configured()` / `HasKey()` melden den Zustand, **ohne** das Secret
  preiszugeben (wie `clio.Client.HasToken`).
- Eine `Complete(ctx, Request) (Response, error)`-Methode: baut die HTTP-Anfrage,
  injiziert den Key serverseitig in den Auth-Header, sendet, dekodiert die
  Antwort. Fehlerzustände werden — wie bei `clio` — auf ein kleines
  Status-Vokabular abgebildet (`offline` ohne Key, `unauthorized` bei 401/403,
  `unreachable` bei Transportfehlern), damit das UI sie direkt rendern kann.
- Ein Kontext-Timeout begrenzt jeden Aufruf, damit ein langsamer Anbieter den
  Server nicht hängen lässt (vgl. `connectionTimeout`).

**Provider-Naht.** Empfehlung: Anthropic (Messages-API) als primär dokumentierter
Pfad — die Workbench wird selbst mit diesem Ökosystem gebaut —, aber hinter einer
schmalen internen Schnittstelle, sodass ein OpenAI-kompatibler Endpunkt später
über Konfiguration (Base-URL + Modellname) dazukommen kann. Die endgültige
Anbieter-Wahl ist eine offene Frage (§10).

### 4.2 Der „Auftrag" (Task)

Ein **Auftrag** ist die zentrale Abstraktion: eine einzelne, klar umrissene
Anfrage an die KI mit definiertem Ein- und Ausgang. Konzeptionell ein Tripel:

```
Auftrag = (Kontext, Instruktion, Ausgabevertrag)
```

- **Kontext** — was die KI sehen darf: der aktuelle `Draft` (oder ein Ausschnitt),
  bei analysierenden Aufträgen der gescopte Event-Ausschnitt über
  `scopedEvents(ctx)` (`SCOPE.md`). Der Kontext wird in Go zusammengestellt, nicht
  im Browser.
- **Instruktion** — ein serverseitiges, versioniertes Prompt-Template
  (`text/template`, embedded), das die Aufgabe beschreibt. Templates liegen im
  Repo, sind reviewbar und tragen keinen Nutzer-Input ungefiltert weiter.
- **Ausgabevertrag** — das erwartete Ergebnis als JSON-Schema (z. B. die Form
  eines `Draft`). Strukturierte Ausgabe / Tool-Use des Anbieters wird genutzt,
  damit die Antwort *parsebar* zurückkommt und nicht aus Prosa extrahiert werden
  muss.

Aufträge werden — analog zur Contribution-Registry der Schale (`FRAMEWORK.md`) —
als kleine Deklarationen registriert. Ein neuer Auftrag ist dann: ein Template,
ein Ausgabe-Typ und ein Handler, der Kontext baut und das Ergebnis anwendet.

### 4.3 Datenfluss eines Auftrags

1. Nutzer löst den Auftrag im UI aus (HTMX-POST, z. B. mit einer Beschreibung).
2. Handler baut den Kontext aus `Draft`/Scope und füllt das Instruktions-Template.
3. `internal/ai` sendet die Anfrage; Key serverseitig im Header.
4. Antwort wird gegen den Ausgabevertrag dekodiert → Kandidaten-`Draft`.
5. Kandidat läuft durch `internal/validate` (und ggf. `internal/schemagen`).
6. Ergebnis wird als **Vorschlag** gerendert — Diff/Vorschau, nicht sofort
   gespeichert. Erst die Bestätigung des Nutzers schreibt über `internal/store`.

---

## 5. Key-Ablage & Konfiguration

Im Clio-Stil, als Spiegel der bestehenden Token-Behandlung (`config.go`):

| Variable | Pflicht | Default | Bedeutung |
|---|---|---|---|
| `WORKBENCH_AI_KEY` | nein | — | API-Key des Anbieters. Serverseitig gehalten, **nur im Speicher**, nie an den Browser. |
| `WORKBENCH_AI_URL` | nein | Anbieter-Default | Endpunkt-Basis-URL (für OpenAI-kompatible/selbstgehostete Ziele). |
| `WORKBENCH_AI_MODEL` | nein | sinnvoller Default | Modellname. |

- **Primärquelle ist die Umgebungsvariable** — genau wie `CLIO_API_TOKEN`. Das
  ist der git-ferne, skript-freundliche Default.
- **Optionale Eingabe im UI** (analog zum Connect-Flow für Clio): Der Key kann
  zur Laufzeit gesetzt werden und lebt dann im Speicher des `ai.Client` bis zum
  Neustart. Er wird **nie** zurück an den Browser gegeben (kein
  Vorausfüllen von Feldern mit dem Secret; das UI zeigt nur „Key gesetzt: ja/nein").
- **Keine Persistenz als Default.** Der Key wird bewusst *nicht* in
  `environments.json` oder eine andere Datei geschrieben — konsistent damit, dass
  auch der Clio-Token nie persistiert wird (`model.Environment`: „The token is
  never stored"). Eine optionale, ausdrücklich opt-in gesetzte serverseitige
  Ablage (Datei mit `0600` unter `WORKBENCH_DATA`) ist als spätere Bequemlichkeit
  denkbar, aber eine eigene Entscheidung mit eigenem Trade-off (§10).

Beim Bauen von Fragmenten gilt die bestehende harte Regel weiter: prüfen, dass
weder Key noch Secrets in die Antwort gelangen (`CLAUDE.md` „Token niemals
exponieren").

---

## 6. Aufträge — die konkreten Anwendungsfälle

Geordnet nach Wert und Umsetzungsnähe. Der erste ist der Leitfall.

1. **Modell erzeugen (Leitfall).** Aus einer Beschreibung in natürlicher Sprache
   einen ersten `Draft` vorschlagen: Knoten/Zustände, Event-Typen als Kanten,
   Start-/Endmarkierungen. Ausgabevertrag ist die `Draft`-Form; der Vorschlag
   durchläuft sofort die Validierung (§4.3) und wird als Vorschau gezeigt.
2. **Schema-Inferenz pro Event-Typ.** Für eine Kante ohne `data`-Schema ein
   plausibles JSON-Schema (bzw. `Field`-Liste) vorschlagen — die Validierung
   markiert solche Lücken ohnehin (`WORKBENCH.md` §5.4), die KI füllt sie als
   Entwurf.
3. **Benennung & Doku.** Konsistente Event-Typ-Namen im Namensraum vorschlagen,
   Beschreibungen/Invarianten formulieren, eine erzählende Modell-Doku als
   Ergänzung zum generierten Export (`WORKBENCH.md` §6.2).
4. **Gegenprobe erklären.** Eine Soll/Ist-Abweichung (`WORKBENCH.md` §7) in Prosa
   einordnen: welche realen Übergänge dem Entwurf widersprechen, welche Pfade tot
   sind — als Lesehilfe auf bereits berechneten Fakten, nicht als Ersatz der
   `internal/validate`-Engine.
5. **Szenarien vorschlagen.** Zum Teststudio (`TESTSTUDIO.md`) konforme und
   bewusst kaputte Fälle als Startpunkt generieren.

Alle teilen denselben Mechanismus aus §4 — sie unterscheiden sich nur in Kontext,
Template und Ausgabevertrag.

---

## 7. UI-Einbindung

Die KI hängt sich über das bestehende Schalen-Muster ein (`FRAMEWORK.md`), ohne
Sonderweg:

- **Auslösen am Ort der Arbeit.** Wo ein Auftrag Sinn ergibt, sitzt ein
  unaufdringlicher Knopf — „Modell aus Beschreibung" in der Modell-Sidebar,
  „Schema vorschlagen" am Kanten-Properties-Panel. HTMX-POST auf den
  Auftrags-Handler, Ergebnis als Fragment.
- **Vorschlag vor Übernahme.** Ergebnisse erscheinen als Diff/Vorschau mit
  „Übernehmen / Verwerfen" — der Mensch entscheidet, der `store` schreibt erst
  danach.
- **KI-Status in der Statusleiste.** Analog zur Verbindungsanzeige für Clio: ein
  schlichter Indikator „KI: bereit / kein Key / nicht erreichbar", der den Key
  nie zeigt.
- **Sauberer Aus-Zustand.** Ohne Key sind die Auftrags-Knöpfe deaktiviert mit
  einem Hinweis, wie man einen Key hinterlegt — das Werkzeug bleibt ohne KI voll
  benutzbar.

---

## 8. Entscheidungen (ADR-artig)

Verdichtete Entscheidungen, im Format Kontext → Entscheidung → Konsequenz →
Verworfene Alternative.

### Entscheidung 1 — KI-Aufrufe laufen serverseitig

- **Kontext.** Die Wertschöpfung (Vorschlag → `Draft` → Validierung → Export)
  lebt in Go (`internal/model`, `validate`, `schemagen`). Prinzip 2 will Logik in
  Go, Prinzip 4 das Secret serverseitig.
- **Entscheidung.** Prompt-Bau, Anbieter-Aufruf und Antwort-Übersetzung liegen im
  Backend (`internal/ai` + Handler in `internal/server`). Das Frontend löst nur
  aus und rendert.
- **Konsequenz.** Kein KI-Code im Browser; die ganze Übersetzungs- und
  Prüflogik bleibt testbar in Go; keine neue Frontend-Toolchain.
- **Verworfen.** KI-Logik im Browser-JS — bräuchte einen Build-Step oder würde
  die Logik aus Go herausziehen (gegen Prinzip 1/2).

### Entscheidung 2 — Der Key bleibt serverseitig und nur im Speicher

- **Kontext.** Der Key ist ein Secret; die Sorge „muss ich dem Server vertrauen?"
  ist berechtigt (§3). Client-seitig scheint die Sorge zu lösen.
- **Entscheidung.** Key serverseitig, primär aus `WORKBENCH_AI_KEY`, optional zur
  Laufzeit gesetzt, **nicht persistiert**, nie an den Browser. Client-seitige
  Ablage wird verworfen.
- **Konsequenz.** Für das lokale Tool (Fall A) kollabiert die Vertrauensfrage;
  Prinzip 4 bleibt unangetastet; keine CORS-/Direct-Browser-Sonderfälle.
- **Verworfen.** Key im Browser/`localStorage` — löst das Vertrauensproblem nicht
  (der Server liefert ohnehin das JS aus, §3.2), bricht Prinzip 4 und arbeitet
  gegen Prinzip 2.

### Entscheidung 3 — Kein SDK, nur `net/http`

- **Kontext.** `go.mod` soll schlank bleiben (`CLAUDE.md`), Produktivcode nutzt
  die Standardbibliothek.
- **Entscheidung.** Der KI-Client spricht die HTTP-API direkt über `net/http` +
  `encoding/json`, wie `internal/clio`.
- **Konsequenz.** Keine neue Abhängigkeit; volle Kontrolle über Header, Timeouts,
  Streaming; testbar mit `net/http/httptest`.
- **Verworfen.** Ein Anbieter-SDK — neue Abhängigkeit ohne hinreichenden Grund.

### Entscheidung 4 — KI-Output ist Vorschlag, nicht Artefakt

- **Kontext.** Prinzip 5: der Entwurf ist die Quelle der Wahrheit.
- **Entscheidung.** Jeder KI-Vorschlag durchläuft `internal/validate` und wird
  als Vorschau gezeigt; erst die Bestätigung schreibt in den `store`.
- **Konsequenz.** Halluzinationen/kaputte Schemas erreichen nie ungeprüft den
  Entwurf; der Mensch bleibt in der Schleife.
- **Verworfen.** KI schreibt direkt in Store/Clio — gibt die Quelle der Wahrheit
  an ein nichtdeterministisches System ab.

---

## 9. Roadmap (Clio-Stufenlogik)

In die Stufenlogik (`WORKBENCH.md` §8) eingeordnet — die KI ist eine
*Querschnitts­fähigkeit*, die auf bestehenden Funktionen aufsetzt:

- **Stufe A — Naht.** `internal/ai`-Client (net/http, Provider-Naht), Key aus
  `WORKBENCH_AI_KEY`, KI-Status in der Statusleiste, „kein Key"-Aus-Zustand.
- **Stufe B — erster Auftrag.** „Modell aus Beschreibung erzeugen" (Leitfall):
  Auftrags-Registry, Kontext-Bau, Ausgabevertrag = `Draft`, Validierung,
  Vorschau-vor-Übernahme.
- **Stufe C — weitere Aufträge.** Schema-Inferenz, Benennung/Doku — über
  denselben Mechanismus.
- **Stufe D — analysierende Aufträge.** Gegenprobe erklären, Szenarien
  vorschlagen; nutzt `scopedEvents` und das Teststudio.

---

## 10. Offene Fragen

1. **Anbieter-Festlegung.** Anthropic-first hinter einer Provider-Naht
   (Empfehlung) vs. von Anfang an anbieter-agnostisch (OpenAI-kompatibel) vs.
   bewusst nur ein Anbieter. Beeinflusst Konfig-Fläche und Test-Doubles.
2. **Optionale Key-Persistenz.** Soll die opt-in-Datei-Ablage (`0600` unter
   `WORKBENCH_DATA`) kommen — Bequemlichkeit gegen den bewussten Bruch mit dem
   „nie persistieren"-Präzedenzfall des Clio-Tokens?
3. **Streaming.** Lohnt sich Token-Streaming (SSE ins UI) für lange Antworten,
   oder genügt eine blockierende Antwort mit Timeout und Spinner?
4. **Kostenschranken.** Braucht es serverseitige Leitplanken (max. Tokens pro
   Auftrag, einfache Ratenbegrenzung), damit ein Auftrag nicht ungewollt teuer
   wird?
5. **Prompt-Injection aus Event-Daten.** Analysierende Aufträge geben reale
   Event-Payloads an die KI. Wie wird verhindert, dass darin eingebettete
   Instruktionen das Template kapern (klare Trennung von Daten und Instruktion,
   Kennzeichnung untrusted Inhalts)?
6. **Reproduzierbarkeit.** Sollen Auftrag, Template-Version und Anbieter-Antwort
   für Nachvollziehbarkeit (z. B. neben dem Draft) festgehalten werden, ohne den
   Key zu berühren?
