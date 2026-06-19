// process.js — an Obsidian-style interactive viewer for the discovered process
// graph: mouse-wheel zoom, background pan, continuously floating force layout,
// node dragging, and hover highlighting of a node's neighbourhood.
//
// Progressive enhancement over the server-rendered SVG; vanilla, embedded, no
// CDN (docs/WORKBENCH.md §2). Without JS the static graph still shows.
(function () {
  "use strict";

  // Live graph instances, so a graph that htmx swaps out of the DOM (innerHTML
  // replaces the whole <svg>, so the per-svg _procStop cleanup never runs on it)
  // can still have its replay timer / RAF loop / listeners torn down. Otherwise
  // an orphaned replay keeps dispatching "clio:replay-step" on document and the
  // inspector keeps scrolling to phantom events long after the graph is gone.
  var instances = [];
  function sweep() {
    for (var i = instances.length - 1; i >= 0; i--) {
      if (!document.contains(instances[i].graph)) {
        try { instances[i].stop(); } catch (e) {}
        instances.splice(i, 1);
      }
    }
  }

  // Force tuning (SVG user units). LEN/REP are generous so nodes sit far enough
  // apart that the (long) event-type labels below them don't overlap.
  var REP = 55000, SPRING = 0.01, LEN = 320, CENTER = 0.0016;
  var COLPAD = 28, COL = 0.5, DAMP = 0.82, VMAX = 18;
  var COOL = 0.96, REST = 0.02;
  var ZMIN = 0.3, ZMAX = 4;

  // edgePath returns [d, labelX, labelY]. Edges attach to the node boundary
  // along the straight line between the two centres, so they leave and enter
  // pointing at each other (no fixed left/right stubs that bow). A single edge
  // is a straight line; a pair of opposite edges (bend) bows gently to opposite
  // sides so both stay legible. Self-loops loop above the node.
  function edgePath(f, t, bend) {
    if (f === t) {
      var x = f.x, y = f.y - f.r;
      return ["M" + (x - 9) + " " + y + " C" + (x - 46) + " " + (y - 58) +
        " " + (x + 46) + " " + (y - 58) + " " + (x + 9) + " " + y, x, y - 50];
    }
    var dx = t.x - f.x, dy = t.y - f.y;
    var dist = Math.sqrt(dx * dx + dy * dy) || 0.01;
    var ux = dx / dist, uy = dy / dist; // unit vector f → t
    var px = -uy, py = ux;              // left-hand normal
    var x1 = f.x + ux * f.r, y1 = f.y + uy * f.r;
    var x2 = t.x - ux * t.r, y2 = t.y - uy * t.r;
    var off = bend ? Math.min(dist * 0.18, 48) : 0;
    var mx = (x1 + x2) / 2 + px * off, my = (y1 + y2) / 2 + py * off;
    var loff = off * 0.5 + 9;
    return ["M" + x1 + " " + y1 + " Q" + mx + " " + my + " " + x2 + " " + y2,
      (x1 + x2) / 2 + px * loff, (y1 + y2) / 2 + py * loff];
  }

  // streamColor maps a subject to a stable, vivid hue so every event of the same
  // stream (one employee's process) is tinted alike. MUST stay byte-for-byte in
  // sync with the copies in subjects.js and the (now removed) local one below.
  function streamColor(s) {
    var h = 0;
    for (var i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) % 360;
    return "hsl(" + h + ", 90%, 64%)";
  }

  // groupBySubject returns a stable reordering of the time-ordered replay where
  // all events of one subject run consecutively (subjects in first-seen order,
  // events within a subject keep their time order). It is the "by subject"
  // replay schedule.
  function groupBySubject(arr) {
    var by = {}, order = [];
    for (var i = 0; i < arr.length; i++) {
      var s = arr[i].s;
      if (!(s in by)) { by[s] = []; order.push(s); }
      by[s].push(arr[i]);
    }
    var out = [];
    for (var k = 0; k < order.length; k++) {
      var g = by[order[k]];
      for (var j = 0; j < g.length; j++) out.push(g[j]);
    }
    return out;
  }

  // openInspector loads the event list for a type into the drawer.
  function openInspector(type) {
    var insp = document.getElementById("inspector");
    if (!insp || !window.htmx) return;
    insp.classList.add("open");
    window.htmx.ajax("GET", "/node-events?type=" + encodeURIComponent(type),
      { target: "#inspector", swap: "innerHTML" });
  }

  function init(graph) {
    var svg = graph.querySelector("svg");
    var viewport = graph.querySelector(".proc-viewport");
    if (!svg || !viewport || !svg.viewBox || !svg.viewBox.baseVal) return;
    if (svg._procStop) svg._procStop();
    for (var ri = instances.length - 1; ri >= 0; ri--) {
      if (instances[ri].graph === graph) instances.splice(ri, 1);
    }
    var ac = new AbortController(), sig = ac.signal;
    var playTimer = null;

    var W = svg.viewBox.baseVal.width, H = svg.viewBox.baseVal.height;
    var cx = W / 2, cy = H / 2;

    var nodes = [], byType = {};
    viewport.querySelectorAll(".proc-node").forEach(function (g) {
      var orb = g.querySelector(".proc-orb");
      var n = {
        el: g, type: g.getAttribute("data-type"), task: g.getAttribute("data-task"),
        ox: parseFloat(orb.getAttribute("cx")), oy: parseFloat(orb.getAttribute("cy")),
        r: parseFloat(orb.getAttribute("r")), vx: 0, vy: 0, fixed: false,
        nbrs: [], inc: [], outE: [],
      };
      n.x = n.ox; n.y = n.oy;
      n.countEl = g.querySelector(".proc-count");
      nodes.push(n); byType[n.type] = n;
    });
    if (!nodes.length) return;

    var edges = [], edgeByKey = {};
    viewport.querySelectorAll(".proc-edge").forEach(function (p) {
      var f = byType[p.getAttribute("data-from")], t = byType[p.getAttribute("data-to")];
      if (!f || !t) return;
      var ed = { from: f, to: t, path: p, label: p.nextElementSibling };
      edges.push(ed);
      edgeByKey[f.type + " -> " + t.type] = ed;
      f.inc.push(ed); t.inc.push(ed); f.outE.push(ed);
      if (f !== t) { f.nbrs.push(t); t.nbrs.push(f); }
    });
    // Bow a pair of opposite edges apart so both stay legible.
    edges.forEach(function (ed) {
      ed.bend = ed.from !== ed.to && !!edgeByKey[ed.to.type + " -> " + ed.from.type];
    });

    // Weight the layout by traffic so the busy "happy path" settles in the
    // centre and rare paths drift outwards: frequent nodes (bigger orbs) are
    // pulled hard to the centre, and heavy edges form a short, tight spine,
    // while rare nodes/edges are only weakly held.
    var rMin = Infinity, rMax = -Infinity;
    nodes.forEach(function (n) { if (n.r < rMin) rMin = n.r; if (n.r > rMax) rMax = n.r; });
    nodes.forEach(function (n) {
      var w = rMax > rMin ? (n.r - rMin) / (rMax - rMin) : 1;
      n.grav = 0.3 + 1.9 * w;
    });
    var maxEW = 1;
    edges.forEach(function (ed) {
      ed.w = ed.label ? (parseInt(ed.label.textContent, 10) || 1) : 1;
      if (ed.w > maxEW) maxEW = ed.w;
    });
    edges.forEach(function (ed) {
      var r = ed.w / maxEW;             // 0..1 share of the busiest flow
      ed.springK = 0.4 + 1.8 * r;       // heavy edges pull harder
      ed.len = LEN * (1.45 - 0.7 * r);  // …and sit shorter (tight central spine)
    });

    // The ordered event stream (shared by the timeline replay and the
    // click-to-focus-a-subject feature below).
    var replay = [];
    var replayScript = graph.querySelector(".proc-replay");
    if (replayScript) {
      try { replay = JSON.parse(replayScript.textContent || "[]"); } catch (e) { replay = []; }
    }

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
      var down = null;
      n.el.addEventListener("pointerdown", function (evt) {
        evt.stopPropagation(); evt.preventDefault();
        down = { x: evt.clientX, y: evt.clientY, t: Date.now() };
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
        // A short, near-stationary press is a click → open the inspector.
        if (down) {
          var dx = evt.clientX - down.x, dy = evt.clientY - down.y;
          if (dx * dx + dy * dy < 25 && Date.now() - down.t < 400) openInspector(n.type);
        }
        down = null;
      }
      n.el.addEventListener("pointerup", drop, { signal: sig });
      n.el.addEventListener("pointercancel", drop, { signal: sig });

      n.el.addEventListener("pointerenter", function () {
        if (dragging) return;
        highlight(n);
      }, { signal: sig });
      n.el.addEventListener("pointerleave", clearHighlight, { signal: sig });
    });

    // Highlight a node's downstream neighbourhood up to the chosen depth.
    var hopsSel = (graph.parentElement || graph).querySelector(".proc-hops select");
    function depth() {
      var v = hopsSel ? hopsSel.value : "1";
      return v === "all" ? Infinity : (parseInt(v, 10) || 1);
    }
    function highlight(node) {
      var d = depth(), seenN = {}, hotN = [node], hotE = [];
      seenN[node.type] = true;
      var frontier = [node], step = 0;
      while (frontier.length && step < d) {
        var next = [];
        frontier.forEach(function (nd) {
          nd.outE.forEach(function (ed) {
            hotE.push(ed);
            if (!seenN[ed.to.type]) { seenN[ed.to.type] = true; hotN.push(ed.to); next.push(ed.to); }
          });
        });
        frontier = next; step++;
      }
      viewport.classList.add("graph-dim");
      hotN.forEach(function (m) { m.el.classList.add("hot"); });
      hotE.forEach(function (ed) { ed.path.classList.add("hot"); if (ed.label) ed.label.classList.add("hot"); });
    }
    function clearHighlight() {
      viewport.classList.remove("graph-dim");
      viewport.querySelectorAll(".hot").forEach(function (el) { el.classList.remove("hot"); });
    }

    // ---- focus a subject: spotlight just one stream's path through the graph ----
    // Clicking a subject in the legend (subjects.js) dispatches "clio:focus-subject".
    // We light up only the nodes and directly-follows edges that subject travels,
    // tinted in its stream colour, and frame them in the viewport.
    var focusedSubject = null;
    function clearFocus() {
      focusedSubject = null;
      viewport.classList.remove("graph-focus");
      viewport.querySelectorAll(".focused").forEach(function (el) {
        el.classList.remove("focused");
        el.style.removeProperty("--stream-glow");
      });
    }
    // frameNodes pans/zooms the viewport so the given nodes fill it (with padding).
    function frameNodes(list) {
      if (!list.length) return;
      var minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
      list.forEach(function (n) {
        minX = Math.min(minX, n.x - n.r); minY = Math.min(minY, n.y - n.r);
        maxX = Math.max(maxX, n.x + n.r); maxY = Math.max(maxY, n.y + n.r);
      });
      var pad = 70;
      minX -= pad; minY -= pad; maxX += pad; maxY += pad;
      var bw = (maxX - minX) || 1, bh = (maxY - minY) || 1;
      var k = Math.max(ZMIN, Math.min(ZMAX, Math.min(W / bw, H / bh)));
      view.k = k;
      view.tx = W / 2 - k * (minX + maxX) / 2;
      view.ty = H / 2 - k * (minY + maxY) / 2;
      applyView();
    }
    function focusSubject(subject) {
      clearFocus();
      if (!subject) return;
      focusedSubject = subject;
      var col = streamColor(subject);
      var nodeSet = {}, edgeSet = {}, prevType = null;
      for (var i = 0; i < replay.length; i++) {
        if (replay[i].s !== subject) continue;
        var t = replay[i].t;
        nodeSet[t] = true;
        if (prevType && prevType !== t) edgeSet[prevType + " -> " + t] = true;
        prevType = t;
      }
      var members = [];
      Object.keys(nodeSet).forEach(function (t) {
        var n = byType[t];
        if (!n) return;
        n.el.classList.add("focused");
        n.el.style.setProperty("--stream-glow", col);
        members.push(n);
      });
      Object.keys(edgeSet).forEach(function (key) {
        var ed = edgeByKey[key];
        if (!ed) return;
        ed.path.classList.add("focused");
        ed.path.style.setProperty("--stream-glow", col);
        if (ed.label) ed.label.classList.add("focused");
      });
      if (!members.length) { clearFocus(); return; }
      viewport.classList.add("graph-focus");
      frameNodes(members);
    }
    document.addEventListener("clio:focus-subject", function (e) {
      var d = e.detail || {};
      focusSubject(d.s || null);
    }, { signal: sig });

    var resetBtn = graph.querySelector(".proc-reset");
    if (resetBtn) {
      resetBtn.addEventListener("click", function () {
        clearFocus();
        view.k = 1; view.tx = 0; view.ty = 0; applyView(); reheat();
      }, { signal: sig });
    }

    // ---- fullscreen: hand the graph element to the Fullscreen API ----
    var fsBtn = graph.querySelector(".proc-fullscreen");
    if (fsBtn) {
      var req = graph.requestFullscreen || graph.webkitRequestFullscreen;
      var exit = function () { return (document.exitFullscreen || document.webkitExitFullscreen).call(document); };
      function fsElement() { return document.fullscreenElement || document.webkitFullscreenElement; }
      if (!req) {
        fsBtn.style.display = "none";
      } else {
        fsBtn.addEventListener("click", function () {
          if (fsElement() === graph) exit();
          else req.call(graph);
        }, { signal: sig });
        var onFsChange = function () {
          var on = fsElement() === graph;
          fsBtn.textContent = on ? "✕" : "⛶";
          fsBtn.title = on ? "Exit fullscreen" : "Toggle fullscreen";
          // The container resized — give the layout a nudge so it re-settles.
          reheat();
        };
        document.addEventListener("fullscreenchange", onFsChange, { signal: sig });
        document.addEventListener("webkitfullscreenchange", onFsChange, { signal: sig });
      }
    }

    // ---- type search: hide non-matching nodes (and their edges) ----
    function applyFilter(q) {
      q = (q || "").trim().toLowerCase();
      var match = {};
      nodes.forEach(function (n) {
        var ok = !q || n.type.toLowerCase().indexOf(q) !== -1;
        match[n.type] = ok;
        n.el.classList.toggle("filtered", !ok);
      });
      edges.forEach(function (ed) {
        var ok = match[ed.from.type] && match[ed.to.type];
        ed.path.classList.toggle("filtered", !ok);
        if (ed.label) ed.label.classList.toggle("filtered", !ok);
      });
    }
    var panel = graph.parentElement;
    var filterInput = panel && panel.querySelector(".proc-filter");
    if (filterInput) {
      filterInput.addEventListener("input", function () { applyFilter(filterInput.value); }, { signal: sig });
      if (filterInput.value) applyFilter(filterInput.value);
    }

    // ---- simulation ----
    var alpha = 1, raf = 0;
    function render() {
      for (var i = 0; i < nodes.length; i++) {
        var n = nodes[i];
        n.el.setAttribute("transform", "translate(" + (n.x - n.ox).toFixed(2) + " " + (n.y - n.oy).toFixed(2) + ")");
      }
      for (var e = 0; e < edges.length; e++) {
        var ed = edges[e], r = edgePath(ed.from, ed.to, ed.bend);
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
        var edge = edges[e], a = edge.from, b = edge.to;
        if (a === b) continue;
        var ex = b.x - a.x, ey = b.y - a.y, el = Math.sqrt(ex * ex + ey * ey) || 0.01;
        var f = (el - edge.len) * SPRING * edge.springK, uex = ex / el, uey = ey / el;
        var ai = nodes.indexOf(a), bi = nodes.indexOf(b);
        fx[ai] += uex * f; fy[ai] += uey * f; fx[bi] -= uex * f; fy[bi] -= uey * f;
      }
      for (var k = 0; k < nodes.length; k++) {
        var n = nodes[k];
        fx[k] += (cx - n.x) * CENTER * n.grav;
        fy[k] += (cy - n.y) * CENTER * n.grav;
        if (n.fixed) { n.vx = 0; n.vy = 0; continue; }
        n.vx = (n.vx + fx[k] * alpha) * DAMP;
        n.vy = (n.vy + fy[k] * alpha) * DAMP;
        n.vx = Math.max(-VMAX, Math.min(VMAX, n.vx));
        n.vy = Math.max(-VMAX, Math.min(VMAX, n.vy));
        n.x += n.vx; n.y += n.vy;
      }
      alpha *= COOL;
    }
    function loop() { step(); render(); raf = alpha > REST ? requestAnimationFrame(loop) : 0; }
    function reheat() { alpha = Math.max(alpha, 0.5); start(); }
    function start() { if (!raf) raf = requestAnimationFrame(loop); }
    function stop() { if (raf) cancelAnimationFrame(raf); raf = 0; }

    document.addEventListener("visibilitychange", function () {
      if (document.hidden) stop(); else start();
    }, { signal: sig });

    // ---- timeline replay: events arrive in order, nodes flash, counts +1 ----
    function setupReplay() {
      var bar = graph.querySelector(".proc-timeline");
      if (!bar) return;
      var N = replay.length;
      var playBtn = bar.querySelector(".tl-play");
      var range = bar.querySelector(".tl-range");
      var label = bar.querySelector(".tl-label");
      var modeBtn = bar.querySelector(".tl-mode");
      if (!N || !playBtn || !range) { bar.style.display = "none"; return; }

      // Two replay schedules over the same events: chronological ("time") and
      // grouped per subject ("subject"). The active one is `seq`; switching
      // resets the cursor since the ordering changed. The choice persists.
      var MODE_KEY = "clio.replayMode";
      var orders = { time: replay, subject: groupBySubject(replay) };
      var mode = localStorage.getItem(MODE_KEY) === "subject" ? "subject" : "time";
      var seq = orders[mode];

      var cursor = N, playing = false, lastBySubject = {};

      function setCount(node, v) { if (node && node.countEl) node.countEl.textContent = v; }
      function flash(el, cls) {
        if (!el) return;
        el.classList.remove(cls);
        requestAnimationFrame(function () { el.classList.add(cls); });
        setTimeout(function () { el.classList.remove(cls); }, 520);
      }

      function recompute(c) {
        var counts = {};
        for (var i = 0; i < c; i++) counts[seq[i].t] = (counts[seq[i].t] || 0) + 1;
        nodes.forEach(function (n) { setCount(n, counts[n.type] || 0); });
        cursor = c; range.value = c;
        label.textContent = c + " / " + N;
      }

      function stepOnce() {
        if (cursor >= N) { stopPlay(); return; }
        var ev = seq[cursor], node = byType[ev.t], col = streamColor(ev.s);
        if (node) {
          var cur = parseInt(node.countEl ? node.countEl.textContent : "0", 10) || 0;
          setCount(node, cur + 1);
          node.el.style.setProperty("--stream-glow", col);
          flash(node.el, "flash");
        }
        var prev = lastBySubject[ev.s];
        if (prev && prev !== ev.t) {
          var ed = edgeByKey[prev + " -> " + ev.t];
          if (ed) { ed.path.style.setProperty("--stream-glow", col); flash(ed.path, "pulse"); }
        }
        lastBySubject[ev.s] = ev.t;
        // Let the inspector follow along (subjects.js / inspector-follow.js).
        document.dispatchEvent(new CustomEvent("clio:replay-step",
          { detail: { s: ev.s, t: ev.t, ts: ev.ts } }));
        cursor++; range.value = cursor;
        label.textContent = cursor + " / " + N + (ev.ts ? "  ·  " + ev.ts : "");
      }

      function play() {
        if (cursor >= N) { lastBySubject = {}; recompute(0); }
        playing = true; playBtn.textContent = "⏸ Pause";
        playTimer = setInterval(stepOnce, 220);
      }
      function stopPlay() {
        if (playTimer) { clearInterval(playTimer); playTimer = null; }
        playing = false; playBtn.textContent = "▶ Replay";
      }

      function setModeLabel() {
        if (!modeBtn) return;
        modeBtn.textContent = mode === "subject" ? "≡ by subject" : "⏱ by time";
        modeBtn.title = mode === "subject"
          ? "Replaying subject by subject — click for chronological order"
          : "Replaying in time order — click to replay subject by subject";
      }

      playBtn.addEventListener("click", function () { if (playing) stopPlay(); else play(); }, { signal: sig });
      range.addEventListener("input", function () { stopPlay(); lastBySubject = {}; recompute(parseInt(range.value, 10) || 0); }, { signal: sig });
      if (modeBtn) {
        setModeLabel();
        modeBtn.addEventListener("click", function () {
          stopPlay();
          mode = mode === "subject" ? "time" : "subject";
          localStorage.setItem(MODE_KEY, mode);
          seq = orders[mode];
          setModeLabel();
          lastBySubject = {}; recompute(0);
        }, { signal: sig });
      }
      label.textContent = N + " / " + N;
    }

    applyView();
    setupReplay();
    svg._procStop = function () { stop(); if (playTimer) clearInterval(playTimer); ac.abort(); svg._procStop = null; };
    // Register so sweep() can stop us if our graph is swapped out of the DOM.
    instances.push({ graph: graph, stop: svg._procStop });
    start();
  }

  function initAll() { document.querySelectorAll(".proc-graph").forEach(init); }

  document.addEventListener("htmx:afterSettle", function (e) {
    // Sweep first: a swap may have removed a (possibly mid-replay) graph even
    // when the new content has no .proc-graph at all (empty/error state).
    sweep();
    if (e.target && e.target.querySelector && e.target.querySelector(".proc-graph")) initAll();
  });
  if (document.readyState !== "loading") initAll();
  else document.addEventListener("DOMContentLoaded", initAll);

  window.ClioProcess = { init: initAll };
})();
