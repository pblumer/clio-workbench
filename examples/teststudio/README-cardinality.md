Cardinality Demo – Employee Identity
====================================

> 🌐 **Language / Sprache:** English (below) · [Deutsch](#kardinalitäts-demo--employee-identity-deutsch)

This example demonstrates **per-subject cardinality** — a rule that lives in the
transition layer, *not* in the JSON Schema of a single event.

The trigger was a real find in the Event Space: the type
`identity.employee.new.v2` was sent **twice for the same subject**
(`/employees/EMP-40008`). A creation event firing twice is a deviation — but
other event types (a temperature reading, a profile update) are *meant* to recur.
The distinction is **how often a type may carry one subject**, and the Workbench
models it as `Edge.Cardinality` (`once` | `many`; empty = unconstrained).

Files
-----

- **draft-employee-identity.json** — the model.
- **suite-employee-identity-tests.json** — four scenarios that exercise the rule.

Import them the same way as the Order Lifecycle demo (sidebar → *Modell
importieren*; Test Studio → *Suite importieren*), or, self-hosted:

  cp draft-employee-identity.json        <DataDir>/
  cp suite-employee-identity-tests.json  <DataDir>/scenarios/employee-identity-tests.json

Draft overview
--------------

Nodes
  none (start) → active

Transitions (event-type edges)
  identity.employee.new.v2             : none   → active   cardinality: once
  identity.employee.email-verified.v1  : active → active   cardinality: once  (self-loop)
  identity.employee.profile-updated.v1 : active → active   cardinality: many  (self-loop)

The two `once` edges show *both* faces of the rule:

- **new.v2** emanates only from the start state, so plain topology already
  forbids a repeat — `once` is here mostly **explicit, exportable intent**.
- **email-verified.v1** is a *self-loop*: the graph would happily take it again.
  Here `once` is the **sole** reason a second occurrence is rejected — exactly the
  case where the annotation earns its keep. `profile-updated.v1` is the contrast:
  a `many` self-loop that recurs freely (the "sensor reading" case).

Suite overview
--------------

"onboarding-with-updates" — valid: new.v2, then one email-verified, then two
  profile-updated. Shows `many` recurring while `once` fires once. Ends in *active*.
"double-creation-emp-40008" — the literal EMP-40008 find: new.v2 twice → rejected
  (here by topology — there is no second new.v2 edge out of *active*).
"double-verification" — new.v2, email-verified, email-verified → rejected
  **specifically by the cardinality rule** ("…email-verified.v1 may occur at most
  once per subject"), since the self-loop topology would otherwise allow it.
"update-without-creation" — profile-updated before any new.v2 → rejected (no
  transition from the start state).

The same engine backs the **Soll/Ist Gegenprobe**: point it at a real instance
and any subject that fires `new.v2` twice surfaces as a deviation — which is how
the EMP-40008 case would be caught against live events.

---

Kardinalitäts-Demo – Employee Identity (Deutsch)
================================================

> 🌐 **Sprache / Language:** Deutsch (unten) · [English](#cardinality-demo--employee-identity)

Dieses Beispiel zeigt die **Kardinalität pro Subject** — eine Regel, die in der
Übergangsschicht lebt, *nicht* im JSON-Schema eines einzelnen Events.

Auslöser war ein echter Fund im Event Space: der Typ
`identity.employee.new.v2` wurde **zweimal auf dasselbe Subject**
(`/employees/EMP-40008`) gesendet. Ein zweimal gefeuertes Anlage-Event ist eine
Abweichung — andere Typen (ein Messwert, eine Profiländerung) *sollen* hingegen
wiederkehren. Die Unterscheidung ist, **wie oft ein Typ ein Subject tragen darf**;
die Workbench modelliert sie als `Edge.Cardinality` (`once` | `many`; leer =
unbeschränkt).

Dateien
-------

- **draft-employee-identity.json** — das Modell.
- **suite-employee-identity-tests.json** — vier Szenarien, die die Regel prüfen.

Import wie bei der Order-Lifecycle-Demo (Sidebar → *Modell importieren*;
Teststudio → *Suite importieren*), oder selbst gehostet:

  cp draft-employee-identity.json        <DataDir>/
  cp suite-employee-identity-tests.json  <DataDir>/scenarios/employee-identity-tests.json

Draft im Überblick
------------------

Knoten
  none (Start) → active

Übergänge (Event-Typ-Kanten)
  identity.employee.new.v2             : none   → active   cardinality: once
  identity.employee.email-verified.v1  : active → active   cardinality: once  (Self-Loop)
  identity.employee.profile-updated.v1 : active → active   cardinality: many  (Self-Loop)

Die beiden `once`-Kanten zeigen *beide* Seiten der Regel:

- **new.v2** geht nur vom Startzustand aus, also verbietet schon die reine
  Topologie eine Wiederholung — `once` ist hier vor allem **explizite,
  exportierbare Absicht**.
- **email-verified.v1** ist ein *Self-Loop*: der Graph würde die Kante erneut
  begehen. Hier ist `once` der **einzige** Grund, warum ein zweites Auftreten
  abgelehnt wird — genau der Fall, in dem die Annotation ihren Wert ausspielt.
  `profile-updated.v1` ist der Kontrast: ein `many`-Self-Loop, der beliebig oft
  wiederkehrt (der „Messwert eines Fühlers").

Suite im Überblick
------------------

"onboarding-with-updates" — gültig: new.v2, dann einmal email-verified, dann zwei
  profile-updated. Zeigt `many` wiederkehrend, `once` einmalig. Endet in *active*.
"double-creation-emp-40008" — der konkrete EMP-40008-Fund: new.v2 zweimal →
  abgelehnt (hier durch die Topologie — aus *active* führt keine zweite
  new.v2-Kante).
"double-verification" — new.v2, email-verified, email-verified → abgelehnt
  **gezielt durch die Kardinalitäts-Regel** („…email-verified.v1 may occur at
  most once per subject"), weil die Self-Loop-Topologie es sonst erlauben würde.
"update-without-creation" — profile-updated vor jedem new.v2 → abgelehnt (kein
  Übergang aus dem Startzustand).

Dieselbe Engine trägt die **Soll/Ist-Gegenprobe**: richtest du sie auf eine echte
Instanz, taucht jedes Subject, das `new.v2` zweimal feuert, als Abweichung auf —
genau so würde der EMP-40008-Fall gegen Live-Events gefunden.
</content>
