// dotted.js — pan/zoom + hover tooltip for the event dotted chart.
// Vanilla, embedded, no CDN (docs/WORKBENCH.md §2). Without JS the chart still
// renders statically.
(function () {
  "use strict";
  var ZMIN = 0.5, ZMAX = 12;

  function init(graph) {
    var svg = graph.querySelector("svg");
    var viewport = graph.querySelector(".dotted-viewport");
    var tip = graph.querySelector(".dot-tip");
    if (!svg || !viewport) return;
    if (svg._dotStop) svg._dotStop();
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

    var pan = null;
    svg.addEventListener("pointerdown", function (evt) {
      if (evt.target.classList.contains("dot")) return;
      pan = { sx: evt.clientX, sy: evt.clientY, tx: view.tx, ty: view.ty };
      graph.classList.add("panning");
      svg.setPointerCapture(evt.pointerId);
    }, { signal: sig });
    svg.addEventListener("pointermove", function (evt) {
      if (!pan) return;
      var m = svg.getScreenCTM();
      view.tx = pan.tx + (evt.clientX - pan.sx) / m.a;
      view.ty = pan.ty + (evt.clientY - pan.sy) / m.d;
      apply();
    }, { signal: sig });
    function endPan(evt) {
      if (!pan) return;
      pan = null; graph.classList.remove("panning");
      try { svg.releasePointerCapture(evt.pointerId); } catch (e) { /* ignore */ }
    }
    svg.addEventListener("pointerup", endPan, { signal: sig });
    svg.addEventListener("pointercancel", endPan, { signal: sig });

    // Tooltip on dot hover (event delegation).
    if (tip) {
      svg.addEventListener("pointerover", function (evt) {
        var dot = evt.target;
        if (!dot.classList || !dot.classList.contains("dot")) return;
        var rect = graph.getBoundingClientRect();
        tip.textContent = dot.getAttribute("data-type") + "  ·  " +
          dot.getAttribute("data-subject") +
          (dot.getAttribute("data-time") ? "  ·  " + dot.getAttribute("data-time") : "");
        tip.style.left = (evt.clientX - rect.left + 12) + "px";
        tip.style.top = (evt.clientY - rect.top + 12) + "px";
        tip.hidden = false;
      }, { signal: sig });
      svg.addEventListener("pointerout", function (evt) {
        if (evt.target.classList && evt.target.classList.contains("dot")) tip.hidden = true;
      }, { signal: sig });
    }

    var reset = graph.querySelector(".proc-reset");
    if (reset) reset.addEventListener("click", function () {
      view = { k: 1, tx: 0, ty: 0 }; apply();
    }, { signal: sig });

    apply();
    svg._dotStop = function () { ac.abort(); svg._dotStop = null; };
  }

  function initAll() { document.querySelectorAll(".dotted-graph").forEach(init); }
  document.addEventListener("htmx:afterSettle", function (e) {
    if (e.target && e.target.querySelector && e.target.querySelector(".dotted-graph")) initAll();
  });
  if (document.readyState !== "loading") initAll();
  else document.addEventListener("DOMContentLoaded", initAll);

  window.ClioDotted = { init: initAll };
})();
