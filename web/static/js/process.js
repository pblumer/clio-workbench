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
  // sync with the copy in subjects.js.
  function streamColor(s) {
    var h = 0;
    for (var i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) % 360;
    return "hsl(" + h + ", 90%, 64%)";
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
    // Stream-Walk (subject-solo) is active: hover-highlight steps aside so it
    // doesn't fight the walk's own dim/lit bookkeeping. Set by setupReplay.
    var walkActive = false;
    // Per-frame hook the simulation calls after laying out the real edges, so
    // the compare-mode overlay edges track the floating nodes. Set by setupReplay.
    var extraRender = null;

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

    // The ordered event stream, parsed once and shared by the timeline replay
    // and the Stream-Walk below.
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
    // frameNodes pans/zooms the viewport so the given nodes fill it (with
    // padding) — used to frame the soloed subject's path in Stream-Walk.
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
      if (walkActive) return; // Stream-Walk owns the spotlight
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
      if (walkActive) return; // leave the Stream-Walk dim in place
      viewport.classList.remove("graph-dim");
      viewport.querySelectorAll(".hot").forEach(function (el) { el.classList.remove("hot"); });
    }

    var resetBtn = graph.querySelector(".proc-reset");
    if (resetBtn) {
      resetBtn.addEventListener("click", function () {
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
      if (extraRender) extraRender(); // compare-overlay edges follow the layout
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
      if (!N || !playBtn || !range) { bar.style.display = "none"; return; }

      var walkBtn = bar.querySelector(".tl-walk");
      var prevBtn = bar.querySelector(".tl-subj-prev");
      var nextBtn = bar.querySelector(".tl-subj-next");

      var cursor = N, playing = false, lastBySubject = {};

      function setCount(node, v) { if (node && node.countEl) node.countEl.textContent = v; }
      function flash(el, cls) {
        if (!el) return;
        el.classList.remove(cls);
        requestAnimationFrame(function () { el.classList.add(cls); });
        setTimeout(function () { el.classList.remove(cls); }, 520);
      }

      function startTimer(fn) { playTimer = setInterval(fn, 220); }
      function clearTimer() { if (playTimer) { clearInterval(playTimer); playTimer = null; } }

      function recompute(c) {
        var counts = {};
        for (var i = 0; i < c; i++) counts[replay[i].t] = (counts[replay[i].t] || 0) + 1;
        nodes.forEach(function (n) { setCount(n, counts[n.type] || 0); });
        cursor = c; range.value = c;
        label.textContent = c + " / " + N;
      }

      function stepOnce() {
        if (cursor >= N) { stopPlay(); return; }
        var ev = replay[cursor], node = byType[ev.t], col = streamColor(ev.s);
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

      // ---- Stream-Walk: solo one subject's trace through the graph ----
      // The whole graph dims; one employee's path lights up step by step in its
      // stream colour, leaving a persistent "ghost trail" of where it has been,
      // and auto-advances to the next subject. See docs/WORKBENCH.md §1.1.
      var bySubject = {}, firstSeen = [];
      for (var si = 0; si < N; si++) {
        var ss = replay[si].s;
        if (!bySubject[ss]) { bySubject[ss] = []; firstSeen.push(ss); }
        bySubject[ss].push(replay[si]);
      }
      // Per-subject variant (its type sequence) and how common that variant is
      // across the population — the raw material for the walk-order modes.
      var variantOf = {}, variantPop = {}, seenIndex = {};
      firstSeen.forEach(function (s, i) {
        seenIndex[s] = i;
        var seq = bySubject[s].map(function (e) { return e.t; }).join(" → ");
        variantOf[s] = seq;
        variantPop[seq] = (variantPop[seq] || 0) + 1;
      });
      // orderSubjects returns the subjects in the chosen walk order:
      //  · chrono  — first-seen, i.e. chronologically by first event (default;
      //              the replay is already timestamp-ordered)
      //  · variant — common variants first, identical journeys adjacent, so
      //              patterns cluster
      //  · rare    — odd-one-out: rarest variant first, anomalies up front
      function orderSubjects(mode) {
        var arr = firstSeen.slice();
        if (mode === "rare") {
          arr.sort(function (a, b) {
            return (variantPop[variantOf[a]] - variantPop[variantOf[b]]) ||
              (bySubject[a].length - bySubject[b].length) || (seenIndex[a] - seenIndex[b]);
          });
        } else if (mode === "variant") {
          arr.sort(function (a, b) {
            return (variantPop[variantOf[b]] - variantPop[variantOf[a]]) ||
              (variantOf[a] < variantOf[b] ? -1 : variantOf[a] > variantOf[b] ? 1 : 0) ||
              (seenIndex[a] - seenIndex[b]);
          });
        }
        return arr; // "chrono" = first-seen order
      }
      var walkOrder = "chrono";
      var subjOrder = orderSubjects(walkOrder);
      var wi = 0, wj = 0, holdTicks = 0, walkLit = [], origMax = range.max;

      function clearWalkLit() {
        for (var i = 0; i < walkLit.length; i++) walkLit[i].classList.remove("walk-lit", "walk-edge");
        walkLit = [];
      }
      function litAdd(el, cls) {
        if (!el || el.classList.contains(cls)) return;
        el.classList.add(cls); walkLit.push(el);
      }
      function walkLabel() {
        var subj = subjOrder[wi] || "", evs = bySubject[subj] || [];
        var ts = wj > 0 && evs[wj - 1] ? evs[wj - 1].ts : (evs[0] ? evs[0].ts : "");
        var unique = variantPop[variantOf[subj]] === 1 ? "  ·  ◇ unique" : "";
        label.textContent = subj + "  ·  " + wj + " / " + evs.length +
          "  ·  [" + (wi + 1) + " / " + subjOrder.length + "]" + unique + (ts ? "  ·  " + ts : "");
      }
      // walkRenderTo paints the current subject's trail up to its j-th event
      // (deterministic — used on load and when scrubbing the range).
      function walkRenderTo(j) {
        var subj = subjOrder[wi], evs = bySubject[subj], col = streamColor(subj);
        if (j < 0) j = 0; if (j > evs.length) j = evs.length;
        clearWalkLit();
        nodes.forEach(function (n) { setCount(n, 0); });
        var prevType = null;
        for (var k = 0; k < j; k++) {
          var node = byType[evs[k].t];
          if (node) {
            setCount(node, (parseInt(node.countEl ? node.countEl.textContent : "0", 10) || 0) + 1);
            node.el.style.setProperty("--stream-glow", col);
            litAdd(node.el, "walk-lit");
          }
          if (prevType && prevType !== evs[k].t) {
            var ed = edgeByKey[prevType + " -> " + evs[k].t];
            if (ed) { ed.path.style.setProperty("--stream-glow", col); litAdd(ed.path, "walk-edge"); }
          }
          prevType = evs[k].t;
        }
        wj = j; range.max = evs.length; range.value = wj; walkLabel();
      }
      function loadSubject(i, full) {
        wi = ((i % subjOrder.length) + subjOrder.length) % subjOrder.length;
        holdTicks = 0;
        walkRenderTo(full ? bySubject[subjOrder[wi]].length : 0);
        updatePinBtn();
        // On a deliberate load (enter/step), frame the subject's path; during
        // auto-play we leave the view put so paths light across the full graph.
        if (full) {
          var seen = {}, members = [];
          bySubject[subjOrder[wi]].forEach(function (ev) {
            if (!seen[ev.t] && byType[ev.t]) { seen[ev.t] = true; members.push(byType[ev.t]); }
          });
          frameNodes(members);
        }
      }
      function walkTick() {
        var subj = subjOrder[wi], evs = bySubject[subj], col = streamColor(subj);
        if (wj >= evs.length) { // subject done — hold a beat, then advance
          if (wi >= subjOrder.length - 1) { stopPlay(); return; }
          if (++holdTicks >= 3) loadSubject(wi + 1, false);
          return;
        }
        var ev = evs[wj], node = byType[ev.t];
        if (node) {
          setCount(node, (parseInt(node.countEl ? node.countEl.textContent : "0", 10) || 0) + 1);
          node.el.style.setProperty("--stream-glow", col);
          litAdd(node.el, "walk-lit");
          flash(node.el, "flash");
        }
        if (wj > 0 && evs[wj - 1].t !== ev.t) {
          var ed = edgeByKey[evs[wj - 1].t + " -> " + ev.t];
          if (ed) { ed.path.style.setProperty("--stream-glow", col); litAdd(ed.path, "walk-edge"); flash(ed.path, "pulse"); }
        }
        document.dispatchEvent(new CustomEvent("clio:replay-step",
          { detail: { s: ev.s, t: ev.t, ts: ev.ts } }));
        wj++; range.value = wj; walkLabel();
      }
      function enterWalk() {
        stopPlay(); clearHighlight(); // drop any lingering hover spotlight first
        walkActive = true;
        viewport.classList.add("graph-dim");
        bar.classList.add("walking");
        walkBtn.textContent = "✕ Solo"; walkBtn.title = "Exit Stream-Walk";
        origMax = range.max;
        loadSubject(0, true); // show the first subject's full trace at a glance
        playBtn.textContent = "▶ Walk";
      }
      function exitWalk() {
        walkActive = false;
        stopPlay(); clearWalkLit(); clearCompare();
        viewport.classList.remove("graph-dim");
        bar.classList.remove("walking");
        walkBtn.textContent = "⛓ Solo"; walkBtn.title = "Stream-Walk: follow one subject through the graph";
        range.max = origMax;
        recompute(N); // restore the full aggregate counts and range
        view.k = 1; view.tx = 0; view.ty = 0; applyView(); reheat(); // un-frame
        playBtn.textContent = "▶ Replay";
      }

      // ---- Compare: pin up to 3 subjects and overlay their full traces ----
      // Each pinned subject is drawn as concentric coloured rings on the nodes it
      // visits (overlap reads as nested rings) plus fanned, colour-matched
      // overlay edges that follow the force layout via extraRender. Built on the
      // walk: you pin whichever subject you are currently soloing.
      var SVGNS = "http://www.w3.org/2000/svg";
      var pinned = [], cmpG = null, cmpEdges = [];
      var pinBtn = bar.querySelector(".tl-pin");
      var pinsBox = graph.querySelector(".proc-pins");

      function cmpLayer() {
        if (!cmpG) {
          cmpG = document.createElementNS(SVGNS, "g");
          cmpG.setAttribute("class", "proc-cmp-edges");
          viewport.insertBefore(cmpG, viewport.firstChild); // beneath the nodes
        }
        return cmpG;
      }
      // cmpPath is edgePath with a per-slot lateral offset so the pinned subjects'
      // shared transitions fan out into parallel, separately-coloured lines.
      function cmpPath(f, t, slot) {
        if (f === t) {
          var x = f.x, y = f.y - f.r;
          return "M" + (x - 9) + " " + y + " C" + (x - 46) + " " + (y - 58) +
            " " + (x + 46) + " " + (y - 58) + " " + (x + 9) + " " + y;
        }
        var dx = t.x - f.x, dy = t.y - f.y, dist = Math.sqrt(dx * dx + dy * dy) || 0.01;
        var ux = dx / dist, uy = dy / dist, px = -uy, py = ux, off = (slot - 1) * 9;
        var x1 = f.x + ux * f.r, y1 = f.y + uy * f.r, x2 = t.x - ux * t.r, y2 = t.y - uy * t.r;
        var mx = (x1 + x2) / 2 + px * off, my = (y1 + y2) / 2 + py * off;
        return "M" + x1 + " " + y1 + " Q" + mx + " " + my + " " + x2 + " " + y2;
      }
      function updateCmpEdges() {
        for (var i = 0; i < cmpEdges.length; i++) {
          cmpEdges[i].path.setAttribute("d", cmpPath(cmpEdges[i].from, cmpEdges[i].to, cmpEdges[i].slot));
        }
      }
      function renderCompare() {
        viewport.querySelectorAll(".cmp-ring").forEach(function (el) { el.remove(); });
        viewport.querySelectorAll(".proc-node.cmp-on").forEach(function (el) { el.classList.remove("cmp-on"); });
        cmpEdges = [];
        if (cmpG) cmpG.textContent = "";
        var ringStack = {}; // node type → rings already drawn there (sets radius)
        pinned.forEach(function (subj, slot) {
          var evs = bySubject[subj], col = streamColor(subj), prevType = null, seenEdge = {};
          evs.forEach(function (ev) {
            var node = byType[ev.t];
            if (node) {
              var k = ringStack[ev.t] || 0;
              if (k <= slot) { // one ring per pinned subject passing through
                var ring = document.createElementNS(SVGNS, "circle");
                ring.setAttribute("class", "cmp-ring");
                ring.setAttribute("cx", node.ox); ring.setAttribute("cy", node.oy);
                ring.setAttribute("r", node.r + 5 + k * 4);
                ring.setAttribute("stroke", col);
                node.el.appendChild(ring);
                node.el.classList.add("cmp-on");
                ringStack[ev.t] = k + 1;
              }
            }
            if (prevType && prevType !== ev.t && byType[prevType] && node) {
              var key = prevType + " -> " + ev.t;
              if (!seenEdge[key]) {
                seenEdge[key] = true;
                var p = document.createElementNS(SVGNS, "path");
                p.setAttribute("class", "cmp-edge"); p.setAttribute("fill", "none");
                p.setAttribute("stroke", col);
                cmpLayer().appendChild(p);
                cmpEdges.push({ path: p, from: byType[prevType], to: node, slot: slot });
              }
            }
            prevType = ev.t;
          });
        });
        extraRender = cmpEdges.length ? updateCmpEdges : null;
        updateCmpEdges();
        renderPins();
        reheat();
      }
      function renderPins() {
        if (!pinsBox) return;
        pinsBox.textContent = "";
        pinned.forEach(function (subj) {
          var chip = document.createElement("button");
          chip.type = "button"; chip.className = "pin-chip"; chip.style.color = streamColor(subj);
          chip.title = "Unpin " + subj;
          var dot = document.createElement("span");
          dot.className = "pin-dot"; dot.style.background = streamColor(subj);
          var nm = document.createElement("span");
          nm.className = "pin-name"; nm.textContent = subj;
          chip.appendChild(dot); chip.appendChild(nm);
          chip.appendChild(document.createTextNode(" ✕"));
          chip.addEventListener("click", function () { unpin(subj); }, { signal: sig });
          pinsBox.appendChild(chip);
        });
        pinsBox.style.display = pinned.length ? "" : "none";
      }
      function isPinned(subj) { return pinned.indexOf(subj) !== -1; }
      function pin(subj) {
        if (isPinned(subj) || pinned.length >= 3) return;
        pinned.push(subj); renderCompare(); updatePinBtn();
      }
      function unpin(subj) {
        var i = pinned.indexOf(subj);
        if (i === -1) return;
        pinned.splice(i, 1); renderCompare(); updatePinBtn();
      }
      function clearCompare() {
        pinned = []; cmpEdges = []; extraRender = null;
        viewport.querySelectorAll(".cmp-ring").forEach(function (el) { el.remove(); });
        viewport.querySelectorAll(".proc-node.cmp-on").forEach(function (el) { el.classList.remove("cmp-on"); });
        if (cmpG) cmpG.textContent = "";
        renderPins(); updatePinBtn();
      }
      function updatePinBtn() {
        if (!pinBtn) return;
        if (isPinned(subjOrder[wi])) { pinBtn.textContent = "📌 Unpin"; pinBtn.disabled = false; }
        else { pinBtn.textContent = "📌 Pin"; pinBtn.disabled = pinned.length >= 3; }
      }

      function play() {
        if (walkActive) {
          if (wj >= bySubject[subjOrder[wi]].length) {
            // at the very end of the last subject → restart the whole tour
            if (wi >= subjOrder.length - 1) loadSubject(0, false);
            else loadSubject(wi, false);
          }
          playing = true; playBtn.textContent = "⏸ Pause";
          startTimer(walkTick);
          return;
        }
        if (cursor >= N) { lastBySubject = {}; recompute(0); }
        playing = true; playBtn.textContent = "⏸ Pause";
        startTimer(stepOnce);
      }
      function stopPlay() {
        clearTimer();
        playing = false;
        playBtn.textContent = walkActive ? "▶ Walk" : "▶ Replay";
      }

      playBtn.addEventListener("click", function () { if (playing) stopPlay(); else play(); }, { signal: sig });
      range.addEventListener("input", function () {
        stopPlay();
        if (walkActive) { walkRenderTo(parseInt(range.value, 10) || 0); return; }
        lastBySubject = {}; recompute(parseInt(range.value, 10) || 0);
      }, { signal: sig });
      if (walkBtn) walkBtn.addEventListener("click", function () { if (walkActive) exitWalk(); else enterWalk(); }, { signal: sig });
      if (prevBtn) prevBtn.addEventListener("click", function () { if (walkActive) { stopPlay(); loadSubject(wi - 1, true); } }, { signal: sig });
      if (nextBtn) nextBtn.addEventListener("click", function () { if (walkActive) { stopPlay(); loadSubject(wi + 1, true); } }, { signal: sig });
      if (pinBtn) pinBtn.addEventListener("click", function () {
        if (!walkActive) return;
        var subj = subjOrder[wi];
        if (isPinned(subj)) unpin(subj); else pin(subj);
      }, { signal: sig });
      var orderSel = bar.querySelector(".tl-order");
      if (orderSel) orderSel.addEventListener("change", function () {
        walkOrder = orderSel.value;
        subjOrder = orderSubjects(walkOrder);
        if (walkActive) { stopPlay(); loadSubject(0, true); }
      }, { signal: sig });
      // ---- keyboard tour: only while soloing, never while typing in a field ----
      document.addEventListener("keydown", function (e) {
        if (!walkActive || e.metaKey || e.ctrlKey || e.altKey) return;
        var tag = (e.target && e.target.tagName || "").toLowerCase();
        if (tag === "input" || tag === "select" || tag === "textarea" || (e.target && e.target.isContentEditable)) return;
        switch (e.key) {
          case " ": case "Spacebar": e.preventDefault(); if (playing) stopPlay(); else play(); break;
          case "j": case "ArrowRight": e.preventDefault(); stopPlay(); loadSubject(wi + 1, true); break;
          case "k": case "ArrowLeft": e.preventDefault(); stopPlay(); loadSubject(wi - 1, true); break;
          case "p": case "P": e.preventDefault();
            if (isPinned(subjOrder[wi])) unpin(subjOrder[wi]); else pin(subjOrder[wi]); break;
          default: return;
        }
      }, { signal: sig });
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
