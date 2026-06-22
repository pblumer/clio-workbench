Lernpfad — Übungsmaterial / Practice material
=============================================

Fertige, importierbare Start-Artefakte zum [Lernpfad](../LEARNING-PATH.md)
([English](../LEARNING-PATH.en.md)). Alle Dateien beziehen sich auf das
Demo-Modell **Order Lifecycle** (`../draft-order-lifecycle.json`).

| Datei | Lektion | Zweck |
|-------|---------|-------|
| `lesson1-payloads.json` | 1 (Schema-Test) | Sammlung gültiger/ungültiger `data`-Payloads je Event-Typ zum Hineinkopieren in den Schema-Test. Jeder Negativfall nennt unter `_why` Feld und Regel. |
| `suite-exercises.json`  | 2 / 3 (Sequenz & Szenarien) | Lauffähige Übungs-Suite mit den **Lösungen** der Lektions-Aufgaben: Storno-nach-Versand (reject), Versand mit ungültigem Carrier (reject) und ein zweiter grüner Vollpfad (accept, `completed`). |

Verwendung
----------

**`lesson1-payloads.json`** ist eine reine Referenz — kein Import nötig. Öffne
den Schema-Test, wähle den Event-Typ und kopiere ein `data`-Objekt hinein. Die
`_about`/`_why`-Felder sind nur Hinweise für dich, kein Teil der Payload.

**`suite-exercises.json`** importierst du wie die Demo-Suite über die GUI — kein
Dateizugriff nötig: Im Tab **Szenarien** (Modell **Order Lifecycle**) unter
**↧ Suite importieren** entweder die Roh-URL der Datei einfügen oder ihren Inhalt
ins Feld **JSON einfügen** kopieren.

Danach erscheint die Suite **„Order Lifecycle – Übungen"** in der Sidebar.
**„Alle prüfen"** muss alle drei Fälle **bestehen** lassen (die beiden
`reject`-Fälle bestehen, *weil* sie korrekt abgelehnt werden).

> **Lokale Alternative (selbst gehostet):** Datei direkt in den Szenario-Store
> legen und neu laden — `cp suite-exercises.json
> "$WORKBENCH_DATA/scenarios/order-lifecycle-exercises.json"`.

> **Hinweis zu `draftRev`:** Die Suite ist gegen den Stand `c6ac54f6d18d` des
> Demo-Modells geschrieben — denselben, den die Demo-Suite nutzt. Änderst du das
> Modell test-relevant, warnt das Studio vor Drift (siehe Lektion 3).

> **Note (English):** `lesson1-payloads.json` is a reference — copy a `data`
> object into the schema-test input. Import `suite-exercises.json` via the GUI
> (Scenarios tab → **↧ Suite importieren** → paste the raw URL or its JSON); no
> filesystem access needed. Self-hosted, you may instead drop it into
> `$WORKBENCH_DATA/scenarios/`. "Check all" should pass all three cases. The
> suite is written against demo-model revision `c6ac54f6d18d`.
</content>
