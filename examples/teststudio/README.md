Test Studio Demo – Order Lifecycle
=====================================

> 🌐 **Language / Sprache:** English (below) · [Deutsch](#test-studio-demo--order-lifecycle-deutsch)

This example gives a complete, self-contained demonstration of the Test
Studio data model. It contains:

- **draft-order-lifecycle.json** — a model definition for an order entity.
- **suite-order-lifecycle-tests.json** — a scenario suite with five test cases.
- **LEARNING-PATH.md** / **LEARNING-PATH.en.md** — a guided, hands-on learning
  path ([German](LEARNING-PATH.md) · [English](LEARNING-PATH.en.md)) that walks
  you through every Test Studio feature, lesson by lesson, using exactly these
  two files. New to the Studio? Start there.
- **exercises/** — ready-to-run practice artifacts for the learning path:
  per-event-type payload sets and a runnable exercise suite with the lesson
  solutions. See [`exercises/README.md`](exercises/README.md).

New here? Follow the learning path ([German](LEARNING-PATH.md) ·
[English](LEARNING-PATH.en.md)) — it takes you from a single payload check all
the way to pushing against an instance and the Soll/Ist Gegenprobe, step by
step.

Importing
---------

No filesystem access needed — this works against a hosted (SaaS) Workbench too.

**Model.** In the sidebar under **Modelle (Models)**, expand
**↧ Modell importieren (Import model)** and pick one:

- **✦ Demo „Order Lifecycle" laden** — one click (needs internet access).
- **Aus URL importieren (Import from URL)** — paste the raw URL of
  `draft-order-lifecycle.json`.
- **JSON einfügen (Paste JSON)** — paste the file contents. Always works.

**Suite.** In the **Teststudio** activity, **Szenarien (Scenarios)** tab, pick
model **Order Lifecycle**, then expand **↧ Suite importieren (Import suite)** and
load `suite-order-lifecycle-tests.json` the same three ways.

Local alternative (self-hosted only): drop the files straight into the data
store and reload —

  cp draft-order-lifecycle.json        <DataDir>/
  cp suite-order-lifecycle-tests.json  <DataDir>/scenarios/order-lifecycle-tests.json

Draft overview
--------------

Nodes
  cart (start) → placed → paid → shipped → completed (end)
  cancelled (end)

Transitions (event-type edges)
  order-placed      : cart       → placed
  order-paid        : placed     → paid
  order-shipped     : paid       → shipped
  order-delivered   : shipped    → completed
  order-cancelled   : cart       → cancelled
  order-cancelled   : placed     → cancelled

Event-type fields
  order-placed       — customerEmail (string, required, format email)
                       itemsCount    (integer, required)
  order-paid         — amount (number, required)
                       paidAt (datetime, required)
  order-shipped      — trackingId (string, required, format uuid)
                       carrier    (string, required, enum DHL/UPS/FedEx)
  order-delivered    — deliveredAt (datetime, required)
  order-cancelled    — reason (string, required)

Suite overview
--------------

"happy-path-standard" — valid walk ending in *completed*, with all payloads.
"cancel-before-payment" — valid cancellation from *placed*, ending in *cancelled*.
"invalid-sequence-ship-without-pay" — rejected because *order-shipped* has no
  valid transition from *placed* without *order-paid*.
"payload-missing-required-field" — rejected because the payload omits the
  required `customerEmail`.
"payload-type-mismatch" — rejected because `itemsCount` is a string instead of
  an integer.

The suite records `draftRev: c6ac54f6d18d`, so drift detection will fire if the
model is edited without updating the tests.

---

Test Studio Demo – Order Lifecycle (Deutsch)
============================================

> 🌐 **Sprache / Language:** Deutsch (unten) · [English](#test-studio-demo--order-lifecycle)

Dieses Beispiel ist eine vollständige, in sich geschlossene Demonstration des
Test-Studio-Datenmodells. Es enthält:

- **draft-order-lifecycle.json** — eine Modelldefinition für eine Bestell-Entität.
- **suite-order-lifecycle-tests.json** — eine Szenario-Suite mit fünf Testfällen.
- **LEARNING-PATH.md** / **LEARNING-PATH.en.md** — ein geführter, praktischer
  Lernpfad ([Deutsch](LEARNING-PATH.md) · [English](LEARNING-PATH.en.md)), der
  dich Lektion für Lektion durch jede Funktion des Test Studios führt — anhand
  genau dieser beiden Dateien. Neu im Studio? Fang dort an.
- **exercises/** — lauffähige Übungs-Artefakte zum Lernpfad: Payload-Sätze je
  Event-Typ und eine importierbare Übungs-Suite mit den Lektions-Lösungen. Siehe
  [`exercises/README.md`](exercises/README.md).

Neu hier? Folge dem Lernpfad ([Deutsch](LEARNING-PATH.md) ·
[English](LEARNING-PATH.en.md)) — er führt dich vom Prüfen einer einzelnen
Payload bis zum Push gegen eine Instanz und der Soll/Ist-Gegenprobe, Schritt für
Schritt.

Importieren
-----------

Kein Dateizugriff nötig — funktioniert auch gegen eine gehostete (SaaS-)Workbench.

**Modell.** Klappe links unter **Modelle** den Punkt **↧ Modell importieren** auf
und wähle einen Weg:

- **✦ Demo „Order Lifecycle" laden** — ein Klick (braucht Internetzugang).
- **Aus URL importieren** — füge die Roh-URL von `draft-order-lifecycle.json` ein.
- **JSON einfügen** — füge den Dateiinhalt ein. Geht immer.

**Suite.** In der Activity **Teststudio**, Tab **Szenarien**, Modell
**Order Lifecycle** wählen, dann **↧ Suite importieren** aufklappen und
`suite-order-lifecycle-tests.json` auf denselben drei Wegen laden.

Lokale Alternative (nur selbst gehostet): die Dateien direkt in die Datenablage
legen und neu laden —

  cp draft-order-lifecycle.json        <DataDir>/
  cp suite-order-lifecycle-tests.json  <DataDir>/scenarios/order-lifecycle-tests.json

Draft im Überblick
------------------

Knoten
  cart (Start) → placed → paid → shipped → completed (Ende)
  cancelled (Ende)

Übergänge (Event-Typ-Kanten)
  order-placed      : cart       → placed
  order-paid        : placed     → paid
  order-shipped     : paid       → shipped
  order-delivered   : shipped    → completed
  order-cancelled   : cart       → cancelled
  order-cancelled   : placed     → cancelled

Event-Typ-Felder
  order-placed       — customerEmail (string, required, Format email)
                       itemsCount    (integer, required)
  order-paid         — amount (number, required)
                       paidAt (datetime, required)
  order-shipped      — trackingId (string, required, Format uuid)
                       carrier    (string, required, enum DHL/UPS/FedEx)
  order-delivered    — deliveredAt (datetime, required)
  order-cancelled    — reason (string, required)

Suite im Überblick
------------------

"happy-path-standard" — gültiger Durchlauf, endet in *completed*, mit allen
  Payloads.
"cancel-before-payment" — gültige Stornierung aus *placed*, endet in *cancelled*.
"invalid-sequence-ship-without-pay" — abgelehnt, weil *order-shipped* aus
  *placed* ohne *order-paid* keinen gültigen Übergang hat.
"payload-missing-required-field" — abgelehnt, weil die Payload das Pflichtfeld
  `customerEmail` auslässt.
"payload-type-mismatch" — abgelehnt, weil `itemsCount` ein String statt eines
  Integers ist.

Die Suite hält `draftRev: c6ac54f6d18d` fest, sodass die Drift-Erkennung
anschlägt, wenn das Modell bearbeitet wird, ohne die Tests anzupassen.
</content>
