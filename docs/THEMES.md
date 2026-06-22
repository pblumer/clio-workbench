# Themes — der portable Token-Vertrag der Clio-Werkzeuge

> Dieses Dokument beschreibt das Theming-Konzept der Workbench: *wie* Farbe in
> dieser Codebasis funktioniert, *wie* man ein Theme wählt oder hinzufügt und
> *wie* derselbe Vertrag in [`pblumer/clio`](https://github.com/pblumer/clio)
> wiederverwendet werden kann. Es ergänzt Prinzip 6 in
> [`WORKBENCH.md`](WORKBENCH.md) und das UI-Framework in
> [`FRAMEWORK.md`](FRAMEWORK.md).

## 1. Warum überhaupt Themes

Der Space-Look (Sternenfeld, Neon-Cyan, violetter Glow) ist die Identität der
Werkbank — aber nicht jede mag ihn, und nicht jeder Kontext verträgt ihn (heller
Raum, Beamer, Barrierearmut, Behörden-CD). Statt den Look hart zu verdrahten,
ist er **ein wählbares Theme unter mehreren** über einem gemeinsamen
**Token-Vertrag**. Der Vertrag ist die eigentliche Lieferung: Er ist frei von
Workbench-Spezifika und kann unverändert in Clios `/ui` wandern — gemeinsame
Designsprache ohne Code-Kopplung.

Mitgeliefert sind vier Themes:

| Theme    | id       | Charakter                                                      |
|----------|----------|----------------------------------------------------------------|
| Nebula   | `nebula` | **Default.** Der Space-Look: dunkel, Sternenfeld, Neon, Glow.  |
| Aurora   | `aurora` | Hell, kühl, gedämpfte Akzente, kein Sternenfeld, dezenter Glow. |
| Carbon   | `carbon` | Neutraler Dark-Mode: ruhiges Graphit, Stahlblau, kein Glow.     |
| Swiss    | `swiss`  | Mock des Bundes-Styleguide: weiss, Bundesrot, Arial, flach.     |

## 2. Der Vertrag: zwei Lagen Tokens

Alle Farben leben in **einer** Datei: [`web/static/css/themes.css`](../web/static/css/themes.css).
Das Struktur-Stylesheet `workbench.css` setzt **nie** selbst eine Farbe, sondern
konsumiert ausschliesslich Tokens über `var(--…)`. »Theme wechseln« heisst damit
nur: diese Tokens neu belegen — kein Markup, kein zweites Stylesheet, kein
Build-Step (Prinzipien 1 & 2).

### Lage 1 — Kanal-Tripel `--rgb-NAME`

Die Basis jeder Farbe ist ein **RGB-Tripel ohne Alpha**, z. B.

```css
--rgb-primary: 56 225 255;   /* nur "R G B", keine Klammern, kein Alpha */
```

Verbraucher mischen die Transparenz **selbst** am Einsatzort:

```css
box-shadow: 0 0 10px rgb(var(--rgb-primary) / 0.2);
border-color: rgb(var(--rgb-primary) / 0.45);
```

Der Gewinn: Eine Leitfarbe hat in der UI dutzende Alpha-Abstufungen (Glow 0.06,
Rahmen 0.4, Füllung 0.16 …). Über das Tripel folgen **alle** automatisch dem
Theme — ein Theme muss nur ~20 Basistöne setzen, nicht hunderte Einzelwerte. Die
Syntax `rgb(R G B / A)` ist CSS Color 4 und läuft in allen aktuellen Browsern
ohne Polyfill.

### Lage 2 — semantische Tokens

Den Tripeln wird eine **Rolle** gegeben, und ganze Verläufe werden gebündelt:

| Token                              | Rolle                                             |
|------------------------------------|---------------------------------------------------|
| `--bg`, `--bg-panel`               | App-Grund, Panel-Fläche                           |
| `--edge`                           | Rahmen / Trennlinien                              |
| `--neon`, `--neon-dim`             | Leitfarbe (hell/gedämpft)                         |
| `--accent`, `--accent-soft`, `--accent-strong` | Prozesse/Subjekte (Violett-Familie)   |
| `--good`, `--warn`, `--info`       | Status-Phasen ok/Warnung/neutral                  |
| `--danger`, `--danger-soft`, `--danger-strong`, `--danger-deep` | Fehler-Familie       |
| `--text`, `--text-dim`, `--text-bright` | Schrift (normal/gedämpft/auf Füllung)        |
| `--glow`                           | gebündelter Schein (`box-shadow`/`text-shadow`)   |
| `--font-ui`                        | UI-Schrift (Code/Daten bleiben monospaced)        |
| `--backdrop`, `--backdrop-opacity` | gesamter App-Hintergrund (Sternenfeld o. Verlauf) |
| `--canvas-bg`, `--orbit-bg`        | Diagramm-Flächen (Graph/Dotted, Event-Orbit)      |

Markup spricht möglichst diese semantischen Tokens an, nicht die rohen Tripel.
Backdrop und Canvas sind bewusst als *ganze* Verläufe getokent: So kann Nebula
ein Sternenfeld zeichnen, während Aurora denselben Slot mit einem ruhigen
Lichtverlauf füllt — ohne dass `workbench.css` etwas davon weiss.

> **`--glow` als Schalter.** Mehrere Komponenten setzen `box-shadow: var(--glow)`.
> Ein Theme schaltet den Schein global aus, indem es `--glow: 0 0 0 transparent;`
> setzt (Carbon, Swiss) — die Hover-Rückmeldung bleibt über Rahmenfarbe erhalten.

## 3. Umschalten & Persistenz

```
Browser  ──[<select> onchange]──▶  theme.js
                                     ├─ setzt <html data-theme="…">   (sofort)
                                     └─ schreibt Cookie wb-theme        (~1 Jahr)
Server   ──[GET /]──▶ themeFromRequest(r) liest Cookie, validiert,
                      rendert <html data-theme="…">  (FOUC-frei beim Reload)
```

- **Sofort, ohne Reload:** `data-theme` auf `<html>` kaskadiert per CSS auf die
  gesamte Seite; `theme.js` setzt es direkt.
- **Persistent & flackerfrei:** Der Server liest das Cookie und rendert
  `data-theme` schon ins ausgelieferte HTML — beim nächsten Laden erscheint nie
  kurz die falsche Palette.
- **Token-Prinzip gewahrt (Prinzip 4):** Das Theme ist **kein** Geheimnis, nur
  der Clio-Token bleibt serverseitig. Deshalb darf `theme.js` das Cookie
  clientseitig schreiben; der Server **liest und validiert** nur
  (`internal/server/theme.go`). Ein unbekannter oder von Hand verbogener
  Cookie-Wert fällt immer auf den Default (Nebula) zurück.

Die Auswahl steht in der Statusleiste (Hauptschale) und in der Editor-Kopfzeile.

## 4. Ein Theme hinzufügen

Zwei kleine Schritte, sonst nichts:

1. **Palette:** In `themes.css` einen Block `[data-theme="<id>"]` anlegen, der die
   Kanal-Tripel und die wenigen semantischen Sonderfälle (Text, `--edge`,
   `--glow`, `--backdrop`, ggf. `--font-ui`) überschreibt. Nicht gesetzte Tokens
   erben aus `:root` (= Nebula), du musst also nur die Abweichungen schreiben.
2. **Registrierung:** In `internal/server/theme.go` einen Eintrag an
   `themeOptions` anhängen (`{ID: "<id>", Label: "<Anzeige>"}`). Damit erscheint
   das Theme in der Auswahl und wird vom Cookie akzeptiert.

Tests in `internal/server/theme_test.go` prüfen Default-Fallback, Validierung und
das serverseitige Rendern; ergänze dort die neue id.

### Was ein Theme *nicht* ändert

Der Vertrag ist bewusst auf **Farbe und Schrift** beschränkt. Geometrie
(Abstände, `border-radius`, Layout-Raster) lebt strukturell in `workbench.css`
und ist über alle Themes gleich — das hält den Vertrag klein und portabel. Wer
z. B. eckigere Ecken bräuchte, würde dafür eine eigene Token-Achse einführen
(`--radius-*`); das ist heute bewusst nicht Teil des Vertrags.

## 5. Wiederverwendung in `pblumer/clio`

`themes.css` enthält **keine** Workbench-Spezifika — nur den Vertrag. Clios `/ui`
kann ihn so übernehmen:

1. `themes.css` als Asset einbetten (Clio bäckt seine UI ebenfalls ins Binary).
2. Im `/ui`-Markup `<html data-theme="…">` rendern (gleiches Cookie `wb-theme`
   oder ein eigenes — die Leseseite ist drei Zeilen Go, siehe `theme.go`).
3. Clios Komponenten auf **dieselben semantischen Token-Namen** mappen
   (`--bg`, `--neon`, `--danger`, `--glow`, `--backdrop` …). Wo Clio heute feste
   Farben nutzt, werden sie durch `var(--…)`/`rgb(var(--rgb-…) / a)` ersetzt.

Ergebnis: Workbench und Clio teilen **eine** Designsprache und denselben
Theme-Katalog, ohne Code voneinander zu importieren — exakt die Kopplungsfreiheit,
die Prinzip 6 fordert.

## 6. Dateien auf einen Blick

| Datei                                   | Rolle                                            |
|-----------------------------------------|--------------------------------------------------|
| `web/static/css/themes.css`             | **Der Vertrag** + alle Theme-Paletten            |
| `web/static/css/workbench.css`          | Struktur/Layout; konsumiert nur Tokens           |
| `web/static/js/theme.js`                | Umschalten (data-theme) + Cookie schreiben       |
| `internal/server/theme.go`              | Cookie lesen/validieren, Katalog `themeOptions`  |
| `internal/server/theme_test.go`         | Tests: Fallback, Validierung, Rendern            |
| `web/templates/index.html`, `editor.html` | rendern `data-theme` + die Auswahl             |
