// density.js — pan/zoom, a hover card and click-to-drill for the Event Space's
// density overview (docs/SPACE-LOD.md). The grid itself is server-rendered SVG,
// so it shows without JS; this only adds interaction. Vanilla, embedded, no CDN
// (docs/WORKBENCH.md §2).
(function () {
  "use strict";
  var ZMIN = 0.5, ZMAX = 12;

  // Filter keys a drill replaces; everything else (types, needles, source) is
  // carried through, so drilling refines the lens instead of resetting it.
  var SUBJECT_KEYS = { subject: 1, subj: 1, s: 1 };
  var FROM_KEYS = { from: 1, after: 1, lower: 1, min: 1 };
  var TO_KEYS = { to: 1, before: 1, upper: 1, max: 1 };

  function init(graph) {
    var svg = graph.querySelector("svg");
    var viewport = graph.querySelector(".dotted-viewport");
    var card = graph.querySelector(".dot-card");
    if (!svg || !viewport) return;
    if (svg._densStop) svg._densStop();
    var ac = new AbortController(), sig = ac.signal;

    var view = { k: 1, tx: 0, ty: 0 };
    function apply() {
      viewport.setAttribute("transform",
        "translate(" + view.tx.toFixed(2) + " " + view.ty.toFixed(2) + ") scale(" + view.k.toFixed(4) + ")");
    }
    function svgPoint(evt) {
      var pt = svg.createSVGPoint();
      pt.x = evt.clientX; pt.y = evt.clientY;
      return pt.matrixTransform(svg.getScreenCTM().inverse());
    }

    svg.addEventListener("wheel", function (evt) {
      evt.preventDefault();
      var sp = svgPoint(evt);
      var lx = (sp.x - view.tx) / view.k, ly = (sp.y - view.ty) / view.k;
      view.k = Math.max(ZMIN, Math.min(ZMAX, view.k * Math.pow(1.0015, -evt.deltaY)));
      view.tx = sp.x - view.k * lx;
      view.ty = sp.y - view.k * ly;
      apply();
    }, { passive: false, signal: sig });

    var pan = null, moved = false, downCell = null;
    svg.addEventListener("pointerdown", function (evt) {
      downCell = (evt.target.classList && evt.target.classList.contains("dcell")) ? evt.target : null;
      pan = { sx: evt.clientX, sy: evt.clientY, tx: view.tx, ty: view.ty };
      moved = false;
      graph.classList.add("panning");
      svg.setPointerCapture(evt.pointerId);
    }, { signal: sig });
    svg.addEventListener("pointermove", function (evt) {
      if (!pan) return;
      var m = svg.getScreenCTM();
      view.tx = pan.tx + (evt.clientX - pan.sx) / m.a;
      view.ty = pan.ty + (evt.clientY - pan.sy) / m.d;
      if (Math.abs(evt.clientX - pan.sx) + Math.abs(evt.clientY - pan.sy) > 4) moved = true;
      apply();
    }, { signal: sig });
    function endPan(evt) {
      if (!pan) return;
      pan = null; graph.classList.remove("panning");
      try { svg.releasePointerCapture(evt.pointerId); } catch (e) { /* ignore */ }
    }
    svg.addEventListener("pointerup", function (evt) {
      var tap = pan && !moved && downCell; // a click, not the end of a drag
      endPan(evt);
      // Pointer capture (set on pointerdown for panning) retargets the synthetic
      // click event to the <svg>, so a plain click listener never sees the cell.
      // Drill straight from pointerup, guarded by the pan/move bookkeeping so a
      // drag isn't mistaken for a tap.
      if (tap) drill(graph, subjectToken(downCell),
        downCell.getAttribute("data-min"), downCell.getAttribute("data-max"));
    }, { signal: sig });
    svg.addEventListener("pointercancel", endPan, { signal: sig });

    // ---- hover card: the cell's aggregate ----
    var hideTimer = null;
    function placeCard(evt) {
      var rect = graph.getBoundingClientRect();
      var x = evt.clientX - rect.left + 14, y = evt.clientY - rect.top + 14;
      card.style.left = Math.max(6, Math.min(x, rect.width - card.offsetWidth - 8)) + "px";
      card.style.top = Math.max(6, Math.min(y, rect.height - card.offsetHeight - 8)) + "px";
    }
    function phaseOf(cell) {
      var m = (cell.getAttribute("class") || "").match(/dcell-(\w+)/);
      return m ? m[1] : "";
    }
    function showCard(cell, evt) {
      if (!card) return;
      var count = cell.getAttribute("data-count") || "0";
      var prefix = cell.getAttribute("data-prefix") || "";
      card.innerHTML =
        '<div class="evcard-head"><span class="evcard-type">' + esc(count) + ' events</span></div>' +
        '<dl class="evcard-meta"><dt>phase</dt><dd>' + esc(phaseOf(cell)) + '</dd>' +
        (prefix ? '<dt>subject</dt><dd>' + esc(prefix) + '</dd>' : '') +
        '</dl><p class="evcard-note">click to drill in</p>';
      card.hidden = false;
      placeCard(evt);
    }
    function hideCard() { if (card) card.hidden = true; }
    svg.addEventListener("pointerover", function (evt) {
      if (evt.target.classList && evt.target.classList.contains("dcell")) {
        if (hideTimer) { clearTimeout(hideTimer); hideTimer = null; }
        showCard(evt.target, evt);
      }
    }, { signal: sig });
    svg.addEventListener("pointermove", function (evt) {
      if (!pan && !card.hidden && evt.target.classList && evt.target.classList.contains("dcell")) placeCard(evt);
    }, { signal: sig });
    svg.addEventListener("pointerout", function (evt) {
      if (evt.target.classList && evt.target.classList.contains("dcell")) {
        hideTimer = setTimeout(hideCard, 120);
      }
    }, { signal: sig });

    var reset = graph.querySelector(".proc-reset");
    if (reset) reset.addEventListener("click", function () {
      view = { k: 1, tx: 0, ty: 0 }; apply();
    }, { signal: sig });

    apply();
    svg._densStop = function () { ac.abort(); svg._densStop = null; };
  }

  // subjectToken picks the most precise subject narrowing a band offers: an exact
  // lexicographic range (From..To) for a name-contiguous band, falling back to a
  // shared path prefix, or "" when the band is scattered (e.g. a variant band) —
  // then the drill narrows by time alone (docs/SPACE-LOD.md §6).
  function subjectToken(cell) {
    var from = cell.getAttribute("data-sfrom"), to = cell.getAttribute("data-sto");
    if (from && to) return from === to ? from : from + ".." + to;
    return cell.getAttribute("data-prefix") || "";
  }

  // drill rebuilds the space filter so it narrows to one cell: the band's subject
  // (range or prefix) plus the cell's event-id range (from:/to:). Other filter
  // tokens are kept. Re-requesting without mode lets the server auto-pick detail
  // once the slice is small enough — the overview→detail hand-off (§4).
  function drill(graph, subject, min, max) {
    var panel = graph.closest(".events-panel") || document;
    var input = panel.querySelector('.space-filter input[name="q"]');
    var frameSel = panel.querySelector("#space-frame-size");
    var kept = [];
    if (input) {
      input.value.split(/\s+/).forEach(function (tok) {
        if (!tok) return;
        var i = tok.indexOf(":");
        var key = i > 0 ? tok.slice(0, i).toLowerCase() : "";
        if (SUBJECT_KEYS[key] || FROM_KEYS[key] || TO_KEYS[key]) return;
        kept.push(tok);
      });
    }
    if (subject) kept.push("subject:" + subject);
    if (min) kept.push("from:" + min);
    if (max) kept.push("to:" + max);
    var q = kept.join(" ");
    if (input) input.value = q;
    var values = { q: q };
    if (frameSel) values.frame = frameSel.value;
    if (window.htmx) {
      window.htmx.ajax("GET", "/space", { target: "#events-slot", swap: "innerHTML", values: values });
    }
  }

  function esc(s) {
    return (s == null ? "" : String(s)).replace(/[&<>"]/g, function (c) {
      return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c];
    });
  }

  function initAll() { document.querySelectorAll(".density-graph").forEach(init); }
  document.addEventListener("htmx:afterSettle", function (e) {
    if (e.target && e.target.querySelector && e.target.querySelector(".density-graph")) initAll();
  });
  if (document.readyState !== "loading") initAll();
  else document.addEventListener("DOMContentLoaded", initAll);

  window.ClioDensity = { init: initAll };
})();
