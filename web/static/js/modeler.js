// modeler.js — interaction layer for the BPMN-style Modeler canvas.
//
// The server renders the whole canvas as static SVG (modeler.html); this file
// only adds the live gestures bpmn.io users expect: pan (drag the background),
// zoom (wheel + toolbar), select (click a shape → server reloads the canvas with
// ?sel), and drag-to-reorder (drop a shape to move it along the chain). All
// persistence goes through htmx on the shared step endpoints — JS never owns the
// model. Vanilla, embedded, no CDN, consistent with docs/WORKBENCH.md §2.
(function () {
  "use strict";

  var DRAG_THRESHOLD = 4; // px of movement before a press becomes a drag
  var ZOOM_MIN = 0.4, ZOOM_MAX = 3.0;

  // Properties-panel width: remembered globally (like the shell's layout), with
  // a floor and a ceiling that keeps the canvas usable. The Modeler fragment is
  // re-rendered whole on every htmx swap, so the stored width is re-applied on
  // each boot.
  var PROPS_MIN = 240, PROPS_MAX = 900, PROPS_KEY = "wb.mdl.props";

  function lsGet(k) { try { return localStorage.getItem(k); } catch (e) { return null; } }
  function lsSet(k, v) { try { localStorage.setItem(k, v); } catch (e) { /* ignore */ } }

  // Per-draft viewport state, so a re-render (htmx swap) keeps the user's pan/zoom.
  var views = {};

  function viewFor(id) {
    if (!views[id]) views[id] = { k: 1, tx: 0, ty: 0 };
    return views[id];
  }

  // svgPoint maps a client event to viewBox coordinates (before our viewport
  // transform), accounting for viewBox + preserveAspectRatio scaling.
  function svgPoint(svg, evt) {
    var pt = svg.createSVGPoint();
    pt.x = evt.clientX;
    pt.y = evt.clientY;
    var ctm = svg.getScreenCTM();
    if (!ctm) return { x: 0, y: 0 };
    var p = pt.matrixTransform(ctm.inverse());
    return { x: p.x, y: p.y };
  }

  function init(root) {
    if (!root || root._mdlReady) return;
    var svg = root.querySelector(".mdl-svg");
    var vp = root.querySelector(".mdl-viewport");
    if (!svg || !vp) return;
    root._mdlReady = true;

    var draftId = root.dataset.draft || "";
    var view = viewFor(draftId);
    var ac = new AbortController();
    var sig = { signal: ac.signal };

    function apply() {
      vp.setAttribute("transform", "translate(" + view.tx + " " + view.ty + ") scale(" + view.k + ")");
    }
    apply();

    // ── Zoom (wheel, anchored at the cursor) ───────────────────────────────
    svg.addEventListener("wheel", function (e) {
      e.preventDefault();
      var p = svgPoint(svg, e);
      var factor = Math.pow(1.0015, -e.deltaY);
      var k = Math.max(ZOOM_MIN, Math.min(ZOOM_MAX, view.k * factor));
      // Keep the point under the cursor fixed.
      view.tx = p.x - (p.x - view.tx) * (k / view.k);
      view.ty = p.y - (p.y - view.ty) * (k / view.k);
      view.k = k;
      apply();
    }, { passive: false, signal: ac.signal });

    function zoomBy(mult) {
      var k = Math.max(ZOOM_MIN, Math.min(ZOOM_MAX, view.k * mult));
      var cx = svg.clientWidth / 2, cy = svg.clientHeight / 2;
      var p = svgPoint(svg, { clientX: svg.getBoundingClientRect().left + cx, clientY: svg.getBoundingClientRect().top + cy });
      view.tx = p.x - (p.x - view.tx) * (k / view.k);
      view.ty = p.y - (p.y - view.ty) * (k / view.k);
      view.k = k;
      apply();
    }
    on(root, ".mdl-zoom-in", function () { zoomBy(1.2); });
    on(root, ".mdl-zoom-out", function () { zoomBy(1 / 1.2); });
    on(root, ".mdl-zoom-fit", function () { view.k = 1; view.tx = 0; view.ty = 0; apply(); });

    // ── Pointer: pan background, or click / drag a shape ───────────────────
    var mode = null;       // "pan" | "shape"
    var start = null;      // pointer-down viewBox point
    var startView = null;  // viewport state at press
    var shape = null;      // the grabbed <g data-step>
    var stepId = "";
    var moved = false;

    svg.addEventListener("pointerdown", function (e) {
      if (e.button !== 0) return;
      var g = e.target.closest && e.target.closest(".mdl-shape[data-step]");
      start = svgPoint(svg, e);
      moved = false;
      if (g && g.dataset.step) {
        mode = "shape"; shape = g; stepId = g.dataset.step;
      } else {
        mode = "pan"; startView = { tx: view.tx, ty: view.ty };
        root.classList.add("is-panning");
      }
      try { svg.setPointerCapture(e.pointerId); } catch (err) { /* ignore */ }
    }, sig);

    svg.addEventListener("pointermove", function (e) {
      if (!mode) return;
      var p = svgPoint(svg, e);
      if (!moved) {
        var dx = (p.x - start.x) * view.k, dy = (p.y - start.y) * view.k;
        if (Math.abs(dx) + Math.abs(dy) > DRAG_THRESHOLD) moved = true;
      }
      if (!moved) return;
      if (mode === "pan") {
        view.tx = startView.tx + (p.x - start.x) * view.k;
        view.ty = startView.ty + (p.y - start.y) * view.k;
        apply();
      } else if (mode === "shape") {
        // Drag the grabbed shape with the cursor (visual only; order on drop).
        shape.classList.add("is-dragging");
        shape.setAttribute("transform", "translate(" + (p.x - start.x) + " " + (p.y - start.y) + ")");
      }
    }, sig);

    svg.addEventListener("pointerup", function (e) {
      try { svg.releasePointerCapture(e.pointerId); } catch (err) { /* ignore */ }
      root.classList.remove("is-panning");
      if (mode === "shape") {
        if (moved) {
          dropReorder(svg, root, shape, stepId, svgPoint(svg, e).x);
        } else {
          select(root, draftId, stepId);
        }
        if (shape) shape.classList.remove("is-dragging");
      }
      mode = null; shape = null; stepId = ""; moved = false;
    }, sig);

    root._mdlStop = function () { ac.abort(); };
  }

  // dropReorder computes the new index from the drop position and asks the
  // server to move the step there (htmx re-renders the canvas).
  function dropReorder(svg, root, shape, stepId, dropX) {
    var others = Array.prototype.slice.call(root.querySelectorAll(".mdl-shape[data-step]"))
      .filter(function (g) { return g !== shape; });
    var to = 0;
    others.forEach(function (g) {
      var bb = g.getBBox();
      if (bb.x + bb.width / 2 < dropX) to++;
    });
    shape.removeAttribute("transform");
    var draftId = root.dataset.draft || "";
    ajax("POST", "/drafts/" + draftId + "/steps/" + stepId + "/reorder?to=" + to + "&view=modeler&sel=" + stepId);
  }

  // ── Properties panel: drag the gutter (or focus it + ←/→) to resize ───────
  function clampPropsWidth(root, w) {
    var stage = root.querySelector(".mdl-stage");
    var max = PROPS_MAX;
    if (stage) max = Math.min(PROPS_MAX, stage.clientWidth - 260); // leave room for the canvas
    if (max < PROPS_MIN) max = PROPS_MIN;
    return Math.max(PROPS_MIN, Math.min(max, w));
  }

  function setPropsWidth(root, props, w) {
    w = Math.round(clampPropsWidth(root, w));
    props.style.flexBasis = w + "px";
    props.style.width = w + "px";
    return w;
  }

  function initPropsResize(root) {
    var props = root.querySelector(".mdl-props");
    var handle = root.querySelector(".mdl-props-resize");
    if (!props || !handle || handle._mdlReady) return;
    handle._mdlReady = true;

    var stored = parseInt(lsGet(PROPS_KEY), 10);
    if (stored > 0) setPropsWidth(root, props, stored);

    var dragging = false, startX = 0, startW = 0;

    handle.addEventListener("pointerdown", function (e) {
      if (e.button !== 0) return;
      e.preventDefault();
      dragging = true;
      startX = e.clientX;
      startW = props.getBoundingClientRect().width;
      handle.classList.add("is-dragging");
      try { handle.setPointerCapture(e.pointerId); } catch (err) { /* ignore */ }
    });
    handle.addEventListener("pointermove", function (e) {
      if (!dragging) return;
      // The gutter sits on the panel's left edge: dragging left widens it.
      setPropsWidth(root, props, startW - (e.clientX - startX));
    });
    function end(e) {
      if (!dragging) return;
      dragging = false;
      handle.classList.remove("is-dragging");
      try { handle.releasePointerCapture(e.pointerId); } catch (err) { /* ignore */ }
      lsSet(PROPS_KEY, String(Math.round(props.getBoundingClientRect().width)));
    }
    handle.addEventListener("pointerup", end);
    handle.addEventListener("pointercancel", end);

    handle.addEventListener("keydown", function (e) {
      if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
      e.preventDefault();
      var step = (e.shiftKey ? 48 : 16) * (e.key === "ArrowLeft" ? 1 : -1);
      var w = setPropsWidth(root, props, props.getBoundingClientRect().width + step);
      lsSet(PROPS_KEY, String(w));
    });
  }

  function select(root, draftId, stepId) {
    ajax("GET", "/modeler?draft=" + encodeURIComponent(draftId) + "&sel=" + encodeURIComponent(stepId));
  }

  // ajax routes through htmx so the response swaps #modeler-slot consistently.
  function ajax(method, url) {
    if (window.htmx) {
      window.htmx.ajax(method, url, { target: "#modeler-slot", swap: "innerHTML" });
    }
  }

  // on attaches a click handler to a control within root (button is static, so
  // re-bound on each init after a swap).
  function on(root, sel, fn) {
    var el = root.querySelector(sel);
    if (el) el.addEventListener("click", fn);
  }

  // ── Bootstrapping: init now and after every htmx swap of the slot ─────────
  function boot() {
    var root = document.getElementById("modeler-root");
    if (root) { init(root); initPropsResize(root); }
  }

  if (document.readyState !== "loading") boot();
  else document.addEventListener("DOMContentLoaded", boot);

  document.addEventListener("htmx:afterSwap", function (e) {
    if (e.target && (e.target.id === "modeler-slot" || (e.target.querySelector && e.target.querySelector("#modeler-root")))) {
      boot();
    }
  });
})();
