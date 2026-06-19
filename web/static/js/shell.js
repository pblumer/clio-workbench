// shell.js — drives the VS-Code-style Workbench shell chrome: activity bar,
// sidebar (collapse), editor tabs, and the bottom panel (tabs + open/close).
//
// The content of every region is loaded and refreshed by htmx (the body
// templates carry their own hx-get/hx-trigger). This file only toggles
// visibility and remembers the user's layout in localStorage. Vanilla,
// embedded, no CDN — consistent with docs/WORKBENCH.md §2.
(function () {
  "use strict";

  var LS = {
    activity: "wb.activity",
    tab: "wb.tab",
    ptab: "wb.ptab",
    sidebar: "wb.sidebar",
    panel: "wb.panel",
  };

  function get(k) { try { return localStorage.getItem(k); } catch (e) { return null; } }
  function set(k, v) { try { localStorage.setItem(k, v); } catch (e) { /* ignore */ } }

  function shell() { return document.querySelector(".wb-shell"); }
  function all(sel, root) { return Array.prototype.slice.call((root || document).querySelectorAll(sel)); }

  // ── Activity bar ↔ sidebar groups ───────────────────────────────────────
  function selectActivity(id, viaClick) {
    var btns = all(".wb-activity");
    var match = btns.filter(function (b) { return b.dataset.activity === id; })[0];
    if (!match) { match = btns[0]; if (!match) return; id = match.dataset.activity; }

    var sh = shell();
    // Clicking the already-active activity toggles the sidebar (VS Code feel).
    if (viaClick && match.classList.contains("is-active")) {
      setSidebar(sh && sh.dataset.sidebar === "collapsed");
      return;
    }

    btns.forEach(function (b) { b.classList.toggle("is-active", b.dataset.activity === id); });
    all(".wb-activity-group").forEach(function (g) {
      g.classList.toggle("is-active", g.dataset.activityGroup === id);
    });
    var title = document.querySelector(".wb-sidebar-title");
    if (title) title.textContent = match.dataset.title || "";

    if (sh && sh.dataset.sidebar === "collapsed") setSidebar(true);
    set(LS.activity, id);
  }

  function setSidebar(open) {
    var sh = shell(); if (!sh) return;
    sh.dataset.sidebar = open ? "open" : "collapsed";
    set(LS.sidebar, open ? "open" : "collapsed");
  }

  // ── Editor tabs ──────────────────────────────────────────────────────────
  function selectTab(id) {
    var tabs = all(".wb-tab");
    var match = tabs.filter(function (t) { return t.dataset.tab === id; })[0];
    if (!match) { match = tabs[0]; if (!match) return; id = match.dataset.tab; }
    tabs.forEach(function (t) { t.classList.toggle("is-active", t.dataset.tab === id); });
    all(".wb-pane").forEach(function (p) { p.classList.toggle("is-active", p.dataset.pane === id); });
    set(LS.tab, id);
  }

  // ── Bottom panel ──────────────────────────────────────────────────────────
  function setPanel(open) {
    var sh = shell(); if (!sh) return;
    sh.dataset.panel = open ? "open" : "closed";
    set(LS.panel, open ? "open" : "closed");
  }

  function selectPanelTab(id) {
    var tabs = all(".wb-ptab");
    var match = tabs.filter(function (t) { return t.dataset.ptab === id; })[0];
    if (!match) { match = tabs[0]; if (!match) return; id = match.dataset.ptab; }
    tabs.forEach(function (t) { t.classList.toggle("is-active", t.dataset.ptab === id); });
    all(".wb-ppane").forEach(function (p) { p.classList.toggle("is-active", p.dataset.ppane === id); });
    set(LS.ptab, id);
  }

  // ── Wiring ────────────────────────────────────────────────────────────────
  function wire() {
    document.addEventListener("click", function (e) {
      var t = e.target.closest("[data-activity]");
      if (t) { selectActivity(t.dataset.activity, true); return; }

      t = e.target.closest("[data-tab]");
      if (t) { selectTab(t.dataset.tab); return; }

      t = e.target.closest("[data-ptab]");
      if (t) { setPanel(true); selectPanelTab(t.dataset.ptab); return; }

      // Sidebar "Forschung" nav: focus the matching editor tab.
      t = e.target.closest("[data-open-tab]");
      if (t) { selectTab(t.dataset.openTab); return; }

      if (e.target.closest(".wb-collapse")) { setSidebar(false); return; }
      if (e.target.closest(".wb-panel-close")) { setPanel(false); return; }
      if (e.target.closest(".wb-panel-toggle")) {
        var sh = shell();
        setPanel(!(sh && sh.dataset.panel === "open"));
        return;
      }
    });

    // Restore the remembered layout (first paint already marks defaults).
    var a = get(LS.activity); if (a) selectActivity(a);
    var tab = get(LS.tab); if (tab) selectTab(tab);
    var ptab = get(LS.ptab); if (ptab) selectPanelTab(ptab);
    if (get(LS.sidebar) === "collapsed") setSidebar(false);
    if (get(LS.panel) === "open") setPanel(true);
  }

  if (document.readyState !== "loading") wire();
  else document.addEventListener("DOMContentLoaded", wire);
})();
