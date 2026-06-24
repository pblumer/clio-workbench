# Level-of-Detail für den Event Space — Darstellung nach Datendichte

**Status:** `ENTWURF` · **Bezug:** [`WORKBENCH.md`](WORKBENCH.md) §2, [`SCOPE.md`](SCOPE.md),
[`FRAMEWORK.md`](FRAMEWORK.md) · **Code:** `internal/server/space.go`,
`internal/process/dotted.go`, `web/templates/space.html`, `web/static/js/dotted.js`

Der Event Space zeichnet heute **eine Zeile pro Subject** und **einen
SVG-Punkt pro Event** (`space.html` §`.dotted-graph`). Das ist die richtige,
detailreiche Sicht — aber sie skaliert nicht: bei 8 506 Subjects und 65 177
Events kann sie weder alle Zeilen zeigen (170 000 px Höhe) noch alle Punkte als
DOM-Knoten halten (Pan/Zoom wird ab wenigen tausend `<circle>` zäh). Heute
behilft sich die Ansicht mit einem harten Deckel: `dMaxRows = 70`, gefüllt mit
den **aktivsten** Subjects (`dotted.go` §„keep the busiest"). Der lange Schwanz
fällt stumm weg — sichtbar nur als `70 / 8 506` in der Kopfzeile.

Dieses Papier beschreibt, wie die Ansicht stattdessen **die Darstellungsform mit
der Datendichte wechselt** (Semantic Zoom / Level-of-Detail), statt Daten zu
verwerfen.

---

## 1. Leitsatz

**Nicht kappen, sondern verdichten.** Wenn zu viel da ist, wechselt die Ansicht
in eine aggregierte Darstellung, die *alles* zeigt — gröber. Sobald der Nutzer
(per Filter, Drill-down oder Frame) auf eine sinnvolle Menge eingrenzt, kippt sie
automatisch zurück in die detailreiche Punkt-Sicht mit Hover-Card, Klick-Detail
und Pan/Zoom. Der Übergang ist *ein* Kontinuum, keine zwei getrennten Werkzeuge.

Das fügt sich in die Grundprinzipien (`WORKBENCH.md` §2): die Aggregation lebt in
**Go** (Logik serverseitig), die Dichte-Darstellung nutzt die *eine* erlaubte
Vanilla-JS-Nische — ein **`<canvas>`**, kein Build-Step, kein DOM-pro-Event.

---

## 2. Zwei Explosionen, nicht eine

Die Skalierung scheitert an **zwei** Achsen, und jede braucht eine eigene
Antwort:

| Achse | Was explodiert | Warum SVG scheitert | Antwort |
|---|---|---|---|
| **Zeilen** (Subjects) | 8 506 Zeilen | passt physisch nicht auf den Schirm | **Gruppierung / Rollup** |
| **Punkte** (Events) | 65 177 Punkte | DOM-Knoten werden zäh (~5–10 k) | **Aggregation / Rasterung** |

Die Zeilen-Achse ist die härtere: selbst bei perfektem Rendering sind 8 506
Zeilen sinnlos. „Pro Event ein Pixel" hilft nur der Punkt-Achse; die Zeilen
müssen *gebündelt* werden.

---

## 3. Der mathematische Werkzeugkasten

**Punkt-Achse — zu viele Events:**

1. **2D-Dichte-Binning.** Ein Raster `Subject-Band × Zeit-Bucket`; jede Zelle
   zählt Events. O(N) Aufbau, **feste** Ausgabegröße unabhängig von N.
2. **Dynamik-Kompression der Farbe (der entscheidende Trick).** Rohzahlen als
   Farbe erzeugen einen roten Klecks, weil wenige heiße Zellen alles erschlagen.
   Stattdessen **log-Skala** oder **Histogramm-Equalisierung** (Perzentil-Rang
   statt Rohzahl) — so wird der lange Schwanz sichtbar. Dies ist der Unterschied
   zwischen „brauchbar" und „unlesbar" (vgl. Datashader).

**Zeilen-Achse — zu viele Subjects:**

3. **Präfix-Rollup.** Subjects sind Pfade (`/employees/EMP-0008`). Nach
   Präfix-Segmenten zu Meta-Zeilen bündeln; Klick klappt auf. Erste Wahl, weil
   billig und ohne neue Konzepte.
4. **Varianten-Rollup.** Subjects nach ihrer Prozessvariante gruppieren —
   wiederverwendet `internal/process` (Directly-Follows-Graph). Folgestufe.
5. **Count-Band-Buckets.** Aktive Subjects oben einzeln, der Schwanz als ein
   verdichtetes Band.

**Fallback:**

6. **Sampling** (stratifiziert). Billig, aber verlustbehaftet — und muss nach
   `WORKBENCH.md`-Grundsatz **sichtbar beschriftet** werden, nie still kappen.

---

## 4. Drei Stufen (Semantic Zoom)

Die Stufe wird **automatisch** aus den Zählern gewählt, die der Server ohnehin
kennt (`Dotted.Total`, `Dotted.Events`). Ein manueller Override-Toggle erlaubt,
die Automatik zu überstimmen.

```
   viele Subjects / Events                 wenige
   ┌───────────────────┐  Filter/Drill  ┌──────────────┐  Klick   ┌──────────┐
   │ Stufe 1: Dichte   │ ─────────────▶ │ Stufe 2:     │ ───────▶ │ Stufe 3: │
   │ (Canvas-Heatmap)  │ ◀───────────── │ Punkte (SVG) │          │ Event    │
   └───────────────────┘   aufzoomen    └──────────────┘          └──────────┘
```

- **Stufe 1 — Übersicht / Dichte.** Greift bei `Total > dMaxRows` **oder**
  gesprengtem Punkt-Budget. Go aggregiert zu einem Dichte-Raster (gruppierte
  Subject-Bänder × Zeit-Buckets, je Zelle Count + dominante Phase/Typ). Das
  Frontend zeichnet es auf ein `<canvas>` mit log/Perzentil-Farbe. Hover zeigt
  das Zell-Aggregat; **Klick driftet hinein**, indem er den bestehenden
  `q`-Filter setzt (`subject:…`, `from:`/`to:`) — Drill-down = vorhandene
  Filtermaschine, kein neuer Mechanismus.
- **Stufe 2 — Punkte (heute).** Sobald die Auswahl ins Budget passt: die
  bestehende SVG-Dotted-Chart mit Hover-Card, Klick-Detail, Pan/Zoom
  (`dotted.js`, `ZMIN..ZMAX`), Live-Stream und Replay. **Unverändert.**
- **Stufe 3 — Einzel-Event (heute).** Die Detail-Card via `/space/event`.
  **Unverändert.**

---

## 5. Schwellen und Budget

- **Zeilen-Budget:** `dMaxRows` (heute 70) bleibt die Grenze, ab der die
  Zeilen-Achse gruppiert wird — aus einem Deckel wird ein Umschaltpunkt.
- **Punkt-Budget:** ein Ziel-Maximum gezeichneter Dots (Größenordnung ~6 000),
  ab dem auf Dichte umgeschaltet wird, auch wenn die Zeilen passen.
- Beide Schwellen sind Konstanten neben den bestehenden Layout-Konstanten in
  `space.go`; bei Bedarf später konfigurierbar.

---

## 6. Invarianten (was sich nicht ändern darf)

1. **Scope bleibt die Quelle der Wahrheit.** LOD ist reine *Darstellung*; es
   liest dieselben Events durch dieselbe Naht (`scopedEvents`, `SCOPE.md`) und
   verengt nichts. Drill-down wirkt nur über die Disziplin-Linse (`q`-Filter).
2. **Kein stilles Kappen.** Jede Verdichtung wird in der Kopfzeile benannt
   (wie heute `70 / 8 506`), Sampling explizit ausgewiesen.
3. **Ein Binary, kein Build.** Canvas-Rendering in eingebettetem Vanilla-JS;
   Aggregation in Go. Keine neue Abhängigkeit.
4. **Ohne JS nutzbar.** Wie heute rendert der Server eine statische Grundsicht;
   das Canvas reichert nur an.

---

## 7. Schnitt-Plan

Nicht alles auf einmal. Größter Hebel zuerst:

1. **Stufe-1-Dichte-Modus** (Go-Aggregation + Canvas + log/Perzentil-Farbe),
   Zeilen erst per **Präfix-Rollup**, Drill-down über den bestehenden `q`-Filter.
   Löst beide Explosionen in einem Schritt.
2. **Varianten-Rollup** der Zeilen (über `internal/process`).
3. **Konfigurierbare Schwellen** und Override-Toggle-Feinschliff.

Tests tabellengetrieben, nur Standardbibliothek; die Aggregation ist reine
Go-Funktion (wie `BuildDotted`) und damit direkt testbar.
</content>
</invoke>
