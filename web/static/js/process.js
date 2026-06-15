// process.js — an Obsidian-style interactive viewer for the discovered process
// graph: mouse-wheel zoom, background pan, continuously floating force layout,
// node dragging, and hover highlighting of a node's neighbourhood.
//
// Progressive enhancement over the server-rendered SVG; vanilla, embedded, no
// CDN (docs/WORKBENCH.md §2). Without JS the static graph still shows.
(function () {
  "use strict";

  // Force tuning (SVG user units).
  var REP = 24000, SPRING = 0.01, LEN = 160, CENTER = 0.0016;
  var COLPAD = 16, COL = 0.5, DAMP = 0.9, VMAX = 30;
  var FLOOR = 0.05, COOL = 0.99, WANDER = 5;
  var ZMIN = 0.3, ZMAX = 4;

  function edgePath(f, t) {
    if (f === t) {
      var x = f.x, y = f.y - f.r;
      return ["M" + (x - 9) + " " + y + " C" + (x - 46) + " " + (y - 58) +
        " " + (x + 46) + " " + (y - 58) + " " + (x + 9) + " " + y, x, y - 50];
    }
    var dx = t.x - f.x;
    if (dx > 0) {
      var x1 = f.x + f.r, y1 = f.y, x2 = t.x - t.r, y2 = t.y;
      return ["M" + x1 + " " + y1 + " C" + (x1 + dx * 0.4) + " " + y1 +
        " " + (x2 - dx * 0.4) + " " + y2 + " " + x2 + " " + y2,
        (x1 + x2) / 2, (y1 + y2) / 2 - 8];
    }
    var ax = f.x, ay = f.y + f.r, bx = t.x, by = t.y + t.r, dip = 64;
    return ["M" + ax + " " + ay + " C" + ax + " " + (ay + dip) +
      " " + bx + " " + (by + dip) + " " + bx + " " + by,
      (ax + bx) / 2, Math.max(ay, by) + dip - 6];
  }

  function init(graph) {
    var svg = graph.querySelector("svg");
    var viewport = graph.querySelector(".proc-viewport");
    if (!svg || !viewport || !svg.viewBox || !svg.viewBox.baseVal) return;
    if (svg._procStop) svg._procStop();
    var ac = new AbortController(), sig = ac.signal;

    var W = svg.viewBox.baseVal.width, H = svg.viewBox.baseVal.height;
    var cx = W / 2, cy = H / 2;

    var nodes = [], byType = {};
    viewport.querySelectorAll(".proc-node").forEach(function (g) {
      var orb = g.querySelector(".proc-orb");
      var n = {
        el: g, type: g.getAttribute("data-type"), task: g.getAttribute("data-task"),
        ox: parseFloat(orb.getAttribute("cx")), oy: parseFloat(orb.getAttribute("cy")),
        r: parseFloat(orb.getAttribute("r")), vx: 0, vy: 0, fixed: false,
        nbrs: [], inc: [],
      };
      n.x = n.ox; n.y = n.oy;
      nodes.push(n); byType[n.type] = n;
    });
    if (!nodes.length) return;

    var edges = [];
    viewport.querySelectorAll(".proc-edge").forEach(function (p) {
      var f = byType[p.getAttribute("data-from")], t = byType[p.getAttribute("data-to")];
      if (!f || !t) return;
      var ed = { from: f, to: t, path: p, label: p.nextElementSibling };
      edges.push(ed);
      f.inc.push(ed); t.inc.push(ed);
      if (f !== t) { f.nbrs.push(t); t.nbrs.push(f); }
    });

    var groups = [];
    viewport.querySelectorAll(".proc-group").forEach(function (gp) {
      var task = gp.getAttribute("data-task");
      groups.push({
        rect: gp.querySelector("rect"), label: gp.querySelector("text"),
        members: nodes.filter(function (n) { return n.task === task; }),
      });
    });

    // ---- view transform (zoom/pan) ----
    var view = { k: 1, tx: 0, ty: 0 };
    function applyView() {
      viewport.setAttribute("transform",
        "translate(" + view.tx.toFixed(2) + " " + view.ty.toFixed(2) + ") scale(" + view.k.toFixed(4) + ")");
    }
    function svgPoint(evt) {
      var pt = svg.createSVGPoint();
      pt.x = evt.clientX; pt.y = evt.clientY;
      return pt.matrixTransform(svg.getScreenCTM().inverse());
    }
    function localPoint(evt) {
      var sp = svgPoint(evt);
      return { x: (sp.x - view.tx) / view.k, y: (sp.y - view.ty) / view.k };
    }

    svg.addEventListener("wheel", function (evt) {
      evt.preventDefault();
      var sp = svgPoint(evt);
      var lp = { x: (sp.x - view.tx) / view.k, y: (sp.y - view.ty) / view.k };
      var factor = Math.pow(1.0015, -evt.deltaY);
      view.k = Math.max(ZMIN, Math.min(ZMAX, view.k * factor));
      view.tx = sp.x - view.k * lp.x;
      view.ty = sp.y - view.k * lp.y;
      applyView();
    }, { passive: false, signal: sig });

    // ---- background pan ----
    var panning = null;
    svg.addEventListener("pointerdown", function (evt) {
      if (evt.target.closest(".proc-node")) return; // node handles its own drag
      panning = { sx: evt.clientX, sy: evt.clientY, tx: view.tx, ty: view.ty };
      graph.classList.add("panning");
      svg.setPointerCapture(evt.pointerId);
    }, { signal: sig });
    svg.addEventListener("pointermove", function (evt) {
      if (!panning) return;
      var m = svg.getScreenCTM();
      view.tx = panning.tx + (evt.clientX - panning.sx) / m.a;
      view.ty = panning.ty + (evt.clientY - panning.sy) / m.d;
      applyView();
    }, { signal: sig });
    function endPan(evt) {
      if (!panning) return;
      panning = null; graph.classList.remove("panning");
      try { svg.releasePointerCapture(evt.pointerId); } catch (e) { /* ignore */ }
    }
    svg.addEventListener("pointerup", endPan, { signal: sig });
    svg.addEventListener("pointercancel", endPan, { signal: sig });

    // ---- node drag + hover highlight ----
    var dragging = null;
    nodes.forEach(function (n) {
      n.el.addEventListener("pointerdown", function (evt) {
        evt.stopPropagation(); evt.preventDefault();
        dragging = n; n.fixed = true; n.el.classList.add("dragging");
        n.el.setPointerCapture(evt.pointerId);
        reheat();
      }, { signal: sig });
      n.el.addEventListener("pointermove", function (evt) {
        if (dragging !== n) return;
        var lp = localPoint(evt);
        n.x = lp.x; n.y = lp.y; n.vx = 0; n.vy = 0;
        reheat();
      }, { signal: sig });
      function drop(evt) {
        if (dragging !== n) return;
        dragging = null; n.fixed = false; n.el.classList.remove("dragging");
        try { n.el.releasePointerCapture(evt.pointerId); } catch (e) { /* ignore */ }
      }
      n.el.addEventListener("pointerup", drop, { signal: sig });
      n.el.addEventListener("pointercancel", drop, { signal: sig });

      n.el.addEventListener("pointerenter", function () {
        if (dragging) return;
        viewport.classList.add("graph-dim");
        n.el.classList.add("hot");
        n.nbrs.forEach(function (m) { m.el.classList.add("hot"); });
        n.inc.forEach(function (ed) {
          ed.path.classList.add("hot");
          if (ed.label) ed.label.classList.add("hot");
        });
      }, { signal: sig });
      n.el.addEventListener("pointerleave", function () {
        viewport.classList.remove("graph-dim");
        n.el.classList.remove("hot");
        n.nbrs.forEach(function (m) { m.el.classList.remove("hot"); });
        n.inc.forEach(function (ed) {
          ed.path.classList.remove("hot");
          if (ed.label) ed.label.classList.remove("hot");
        });
      }, { signal: sig });
    });

    var resetBtn = graph.querySelector(".proc-reset");
    if (resetBtn) {
      resetBtn.addEventListener("click", function () {
        view.k = 1; view.tx = 0; view.ty = 0; applyView(); reheat();
      }, { signal: sig });
    }

    // ---- simulation ----
    var alpha = 1, raf = 0;
    function render() {
      for (var i = 0; i < nodes.length; i++) {
        var n = nodes[i];
        n.el.setAttribute("transform", "translate(" + (n.x - n.ox).toFixed(2) + " " + (n.y - n.oy).toFixed(2) + ")");
      }
      for (var e = 0; e < edges.length; e++) {
        var ed = edges[e], r = edgePath(ed.from, ed.to);
        ed.path.setAttribute("d", r[0]);
        if (ed.label) { ed.label.setAttribute("x", r[1]); ed.label.setAttribute("y", r[2]); }
      }
      for (var g = 0; g < groups.length; g++) {
        var grp = groups[g];
        if (grp.members.length < 2) continue;
        var minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
        for (var m = 0; m < grp.members.length; m++) {
          var mn = grp.members[m];
          minX = Math.min(minX, mn.x - mn.r); minY = Math.min(minY, mn.y - mn.r);
          maxX = Math.max(maxX, mn.x + mn.r); maxY = Math.max(maxY, mn.y + mn.r);
        }
        var pad = 16, gap = 14;
        grp.rect.setAttribute("x", minX - pad); grp.rect.setAttribute("y", minY - pad - gap);
        grp.rect.setAttribute("width", (maxX - minX) + 2 * pad);
        grp.rect.setAttribute("height", (maxY - minY) + 2 * pad + gap);
        grp.label.setAttribute("x", minX - pad); grp.label.setAttribute("y", minY - pad - gap);
      }
    }
    function step() {
      var fx = new Float64Array(nodes.length), fy = new Float64Array(nodes.length);
      for (var i = 0; i < nodes.length; i++) {
        for (var j = i + 1; j < nodes.length; j++) {
          var dx = nodes[i].x - nodes[j].x, dy = nodes[i].y - nodes[j].y;
          var d2 = dx * dx + dy * dy || 0.01, d = Math.sqrt(d2);
          var ux = dx / d, uy = dy / d, rep = REP / d2;
          fx[i] += ux * rep; fy[i] += uy * rep; fx[j] -= ux * rep; fy[j] -= uy * rep;
          var minD = nodes[i].r + nodes[j].r + COLPAD;
          if (d < minD) {
            var push = (minD - d) * COL;
            fx[i] += ux * push; fy[i] += uy * push; fx[j] -= ux * push; fy[j] -= uy * push;
          }
        }
      }
      for (var e = 0; e < edges.length; e++) {
        var a = edges[e].from, b = edges[e].to;
        if (a === b) continue;
        var ex = b.x - a.x, ey = b.y - a.y, el = Math.sqrt(ex * ex + ey * ey) || 0.01;
        var f = (el - LEN) * SPRING, uex = ex / el, uey = ey / el;
        var ai = nodes.indexOf(a), bi = nodes.indexOf(b);
        fx[ai] += uex * f; fy[ai] += uey * f; fx[bi] -= uex * f; fy[bi] -= uey * f;
      }
      for (var k = 0; k < nodes.length; k++) {
        var n = nodes[k];
        fx[k] += (cx - n.x) * CENTER + (Math.random() - 0.5) * WANDER;
        fy[k] += (cy - n.y) * CENTER + (Math.random() - 0.5) * WANDER;
        if (n.fixed) { n.vx = 0; n.vy = 0; continue; }
        n.vx = (n.vx + fx[k] * alpha) * DAMP;
        n.vy = (n.vy + fy[k] * alpha) * DAMP;
        n.vx = Math.max(-VMAX, Math.min(VMAX, n.vx));
        n.vy = Math.max(-VMAX, Math.min(VMAX, n.vy));
        n.x += n.vx; n.y += n.vy;
      }
      alpha = Math.max(FLOOR, alpha * COOL);
    }
    function loop() { step(); render(); raf = requestAnimationFrame(loop); }
    function reheat() { alpha = Math.max(alpha, 0.5); }
    function start() { if (!raf) raf = requestAnimationFrame(loop); }
    function stop() { if (raf) cancelAnimationFrame(raf); raf = 0; }

    document.addEventListener("visibilitychange", function () {
      if (document.hidden) stop(); else start();
    }, { signal: sig });

    applyView();
    svg._procStop = function () { stop(); ac.abort(); svg._procStop = null; };
    start();
  }

  function initAll() { document.querySelectorAll(".proc-graph").forEach(init); }

  document.addEventListener("htmx:afterSettle", function (e) {
    if (e.target && e.target.querySelector && e.target.querySelector(".proc-graph")) initAll();
  });
  if (document.readyState !== "loading") initAll();
  else document.addEventListener("DOMContentLoaded", initAll);

  window.ClioProcess = { init: initAll };
})();
