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

**`suite-exercises.json`** importierst du wie die Demo-Suite in den
Szenario-Store:

```sh
cp examples/teststudio/exercises/suite-exercises.json \
   "$WORKBENCH_DATA/scenarios/order-lifecycle-exercises.json"
```

Danach erscheint die Suite **„Order Lifecycle – Übungen"** in der Sidebar.
**„Alle prüfen"** muss alle drei Fälle **bestehen** lassen (die beiden
`reject`-Fälle bestehen, *weil* sie korrekt abgelehnt werden).

> **Hinweis zu `draftRev`:** Die Suite ist gegen den Stand `c6ac54f6d18d` des
> Demo-Modells geschrieben — denselben, den die Demo-Suite nutzt. Änderst du das
> Modell test-relevant, warnt das Studio vor Drift (siehe Lektion 3).

> **Note (English):** `lesson1-payloads.json` is a reference — copy a `data`
> object into the schema-test input. Import `suite-exercises.json` into
> `$WORKBENCH_DATA/scenarios/`; "Check all" should pass all three cases. The
> suite is written against demo-model revision `c6ac54f6d18d`.
</content>
