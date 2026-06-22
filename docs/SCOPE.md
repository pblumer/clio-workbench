# Das Scope-Konzept — welche Events die Werkbank betrachtet

**Status:** `STUFE 0` · **Bezug:** [`WORKBENCH.md`](WORKBENCH.md) §3, [`FRAMEWORK.md`](FRAMEWORK.md)

Jede Analyse-Ansicht der Werkbank — der Event Space, der Process-Viewer, die
Relationships, die Gegenprobe — liest Events aus einer Clio-Instanz. Die Frage
*„welche Events eigentlich?"* beantwortet **der Scope**. Dieses Papier legt
fest, was ein Scope ist, woraus er sich zusammensetzt und wie eine Disziplin
(ein Plugin im Sinne der [Contribution-Registry](FRAMEWORK.md)) ihn **global
übernimmt** und zugleich **lokal weiter gestaltet**.

Leitsatz: **Global definierbar, lokal gestaltbar.** Es gibt genau eine globale
Quelle der Wahrheit für „wo und wie viel" — und trotzdem darf jede Ansicht ihren
eigenen Ausschnitt schärfen, ohne andere Ansichten zu beeinflussen.

---

## 1. Die drei Lagen

Ein Scope ist keine flache Einstellung, sondern eine **geschichtete
Komposition** aus drei Lagen. Jede Lage ist ein *Filter*; sie werden mit `UND`
übereinandergelegt:

```
   Environment       (global, persistent)   Server + Basis-Scope + Limit
        │                                    ── definiert das Universum
        ∧
   Queries           (global, Session)       geteilter Verfeinerungs-Trichter
        │                                    ── verengt für alle Ansichten
        ∧
   Disziplin-Linse   (lokal, pro Plugin)     die Eigen-Verfeinerung einer Ansicht
        │                                    ── verengt nur diese eine Ansicht
        ▼
   Effektiver Scope  — die Events, die genau diese Ansicht liest
```

| Lage | Lebensdauer | Persistiert | Erreicht Clio | Setzt das Limit | Geteilt |
|---|---|---|---|---|---|
| **Environment** | über Neustarts | ja (`environments.json`) | **ja** (server-seitige Query) | **ja** | ja (eine aktiv) |
| **Queries** | Session | nein (resettet bei Neustart) | nein (In-Process) | nein | ja (alle Ansichten) |
| **Disziplin-Linse** | pro Request/Ansicht | nein | nein (In-Process) | nein | **nein** (nur diese Ansicht) |

---

## 2. Der Auflösungsvertrag

Drei Regeln machen das Konzept eindeutig — sie gelten immer und überall:

1. **Komposition durch Schnittmenge.**
   `effektiv = Environment ∧ Queries ∧ Linse`. Eine Ansicht sieht genau die
   Events, die *alle* aktiven Lagen passieren.

2. **Monotonie (Trichter-Invariante).** Jede Lage darf nur **verengen**, nie
   erweitern. Eine Ansicht sieht nie mehr Events, als das Environment zulässt.
   Queries verengen, was das Environment liefert; die Linse verengt, was die
   Queries übriglassen. Wer mehr sehen will, lockert das Environment — nicht die
   Linse.

3. **Genau eine Lage erreicht Clio.** Nur das **Environment** wird zu einer
   server-seitigen Read-Query (Subject-Teilbaum, Typen, Id-Range) und legt das
   **Read-Limit** fest. `Queries` und `Linse` sind reine In-Process-Filter über
   dem bereits gelesenen Universum. Daraus folgt: das Limit gehört dem
   Environment (bzw. dem globalen Cap `WORKBENCH_EVENT_CAP`), nie einer
   Verfeinerung.

---

## 3. Die Lagen im Detail

### 3.1 Environment — das Universum (global, persistent)

Ein **Environment** ist ein gespeicherter, umschaltbarer Arbeitskontext: ein
Server plus ein Basis-Scope.

- **Felder:** `ServerURL`, `Subject` (Prefix), `Types`, `LowerBound`,
  `UpperBound`, `Limit` (`0` = globaler Cap).
- **Persistent & umschaltbar:** in `environments.json` gespeichert; genau eines
  ist aktiv (oder „— all data —", dann gilt nur der globale Cap). Das Aktivieren
  eines Environments, das einen Server nennt, schwenkt auch den `/api`-Proxy
  dorthin.
- **Der Token wird nie gespeichert** — er bleibt im Connect-Fluss (siehe
  `README.md`, *Connection status*).
- **Einzige Lage mit Server-Wirkung:** Subject/Types/Bounds werden zur
  Clio-Query, `Limit` kappt den Read. Alles Weitere passiert lokal.

### 3.2 Queries — der geteilte Trichter (global, Session)

Die **Query-Pipeline** ist eine Kette von Verfeinerungs-Stufen, die *nach* dem
Environment greift und **für alle Ansichten gleichzeitig** verengt. Jede Stufe
dezimiert die Überlebenden der vorigen (ein `UND` über die Kette). Das ist das
gemeinsame Forschungswerkzeug: „erst das Environment, dann gezielt
runterbohren".

- **Session-Zustand**, bewusst nicht persistiert: resettet bei Neustart.
- Wirkt auf **jede** Analyse-Ansicht — wer hier eine Stufe hinzufügt, verengt
  Event Space, Process und Relationships zugleich.

### 3.3 Disziplin-Linse — der lokale Ausschnitt (lokal, pro Plugin)

Eine **Disziplin-Linse** ist die *eigene* zusätzliche Verengung einer einzelnen
Ansicht. Sie wirkt **nur auf diese Ansicht** und ist nicht persistent. Hier
lebt das Versprechen „lokal gestaltbar": ohne das globale Environment oder die
geteilten Queries anzutasten, schärft eine Disziplin ihren Blick.

Bereits heute vorhandene Linsen:

| Disziplin | Linse | Quelle |
|---|---|---|
| **Process** | Subject-Prefix + Source-Substring | Query-Param `subject`, `source` |
| **Event Space** | Frame (die letzten *N* Events) | Query-Param `frame` |
| **Gegenprobe** *(geplant)* | Subject-Teilbaum des Soll-Modells | Draft-Auswahl |

Eine Linse ist gegenüber Queries das Spiegelbild: Queries sind **geteilt und
explizit** (man baut sie sichtbar im Umgebung-Panel), Linsen sind **lokal und
ansichtseigen** (sie gehören zu den Bedienelementen der Ansicht selbst).

---

## 4. Die Naht im Code

Alle drei Lagen treffen sich an **einer** Stelle —
[`internal/server/queries.go`](../internal/server/queries.go):

```go
// scopedEvents liest das Universum des aktiven Environments aus Clio und legt
// danach die geteilte Query-Pipeline plus die optionale Disziplin-Linse als
// EINEN UND-Trichter darüber. Jede Analyse-Ansicht geht durch diese Naht.
func (s *Server) scopedEvents(ctx context.Context, lens ...queryStage) ([]clio.Event, error)
func (s *Server) scopedFullEvents(ctx context.Context, lens ...queryStage) ([]clio.FullEvent, error)
```

Das **Verfeinerungs-Primitiv** ist für alle Lagen dasselbe — ein `queryStage`:

```go
type queryStage struct {
    Subject    string   // Subject-Prefix (segmentweise)
    Types      []string // erlaubte Event-Typen
    LowerBound string   // Id-Untergrenze
    UpperBound string   // Id-Obergrenze
    Source     string   // Source-Substring
}
```

So entsteht ein klarer Bauplan: **jede Lage ist eine Liste von `queryStage`s.**
Das Environment ist der Sonderfall, der zusätzlich einen Server und das Limit
trägt und als Einziger zur Clio-Query wird (`activeScope`); Queries und Linse
sind In-Process-Stufen, die `scopedEvents` in genau dieser Reihenfolge
komponiert (`Environment` → `Queries` → `Linse`).

---

## 5. Wie eine Disziplin ihren Scope gestaltet

Analog zu „[eine neue Ansicht einhängen](FRAMEWORK.md)" — die Schritte, um einer
Ansicht eine eigene Linse zu geben:

1. **Bedienelemente** der Ansicht liefern die lokalen Filter (Query-Params,
   ein kleines Formular). Sie gehören der Ansicht, nicht dem Umgebung-Panel.

2. **Linse bauen** — die lokalen Filter in einen (oder mehrere) `queryStage`
   übersetzen:

   ```go
   lens := queryStage{Subject: subject, Source: source}
   ```

3. **Durch die Naht lesen** — die Linse an `scopedEvents` übergeben; den Rest
   (Environment-Read, Limit, geteilte Queries) erledigt der Server:

   ```go
   events, err := s.scopedEvents(ctx, lens)
   ```

4. **Effektiven Scope zeigen.** Die Ansicht macht sichtbar, was sie betrachtet
   (aktives Environment, Stufenzahl der Queries, eigene Linse) — der Nutzer soll
   nie raten, welcher Ausschnitt gerade gilt.

Eine Ansicht **ohne** eigene Linse ruft `scopedEvents(ctx)` ohne Argumente auf
und erbt damit exakt Environment + Queries — der Normalfall.

---

## 6. Bewusste Grenzen

- **Eine Linse leckt nicht.** Sie verengt nur ihre eigene Ansicht; zwei
  Disziplinen können denselben geteilten Scope völlig verschieden ausschneiden.
- **Queries sind nicht persistent.** Wiederkehrende Verengungen, die man behalten
  will, gehören als eigenes **Environment** gespeichert, nicht in die Pipeline.
- **Das Limit ist kein Verfeinerungswerkzeug.** Es schützt vor Über-Reads und
  gehört darum allein dem Environment. Eine Linse, die „weniger Events" will,
  filtert — sie senkt nicht das Limit.

---

## 7. Verhältnis zur restlichen Architektur

- [`WORKBENCH.md`](WORKBENCH.md) §1.1 beschreibt die Werkbank als *Labor* zum
  Durchforschen echter Event-Ströme. Der Scope ist das Instrument, mit dem man
  den Ausschnitt für ein Experiment wählt.
- [`FRAMEWORK.md`](FRAMEWORK.md) erklärt die Schale und wie Ansichten eingehängt
  werden; dieses Papier ergänzt, **welche Daten** eine eingehängte Ansicht sieht
  und wie sie diesen Ausschnitt mitgestaltet.
- Die **Gegenprobe** (`WORKBENCH.md` §7) ist der Spezialfall, bei dem die Linse
  vom Soll-Modell abgeleitet wird: der Subject-Teilbaum des Entwurfs schneidet
  das reale Universum auf das, was dem Modell entsprechen soll.
</content>
</invoke>
