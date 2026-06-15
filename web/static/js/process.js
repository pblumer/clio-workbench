// process.js — make the discovered process graph floating & draggable.
//
// Progressive enhancement over the server-rendered SVG: read the laid-out
// nodes/edges, then run a tiny force simulation so nodes repel ("nudge") each
// other and you can drag them around; edges and task backdrops follow live.
// Vanilla, embedded, no CDN (docs/WORKBENCH.md §2). Without JS the static
// graph still shows.
(function () {
  "use strict";

  // Force tuning (SVG user units).
  var REP = 22000;   // node repulsion
  var SPRING = 0.01; // edge attraction
  var LEN = 150;     // desired edge length
  var CENTER = 0.002;
  var COLPAD = 16;   // extra spacing before collision kicks in
  var COL = 0.5;     // collision shove strength
  var DAMP = 0.9;
  var COOL = 0.985;
  var VMAX = 28;

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
    var bx1 = f.x, by1 = f.y + f.r, bx2 = t.x, by2 = t.y + t.r, dip = 64;
    var mid = Math.max(by1, by2);
    return ["M" + bx1 + " " + by1 + " C" + bx1 + " " + (by1 + dip) +
      " " + bx2 + " " + (by2 + dip) + " " + bx2 + " " + by2,
      (bx1 + bx2) / 2, mid + dip - 6];
  }

  function init(graph) {
    var svg = graph.querySelector("svg");
    if (!svg || !svg.viewBox || !svg.viewBox.baseVal) return;
    if (svg._procStop) svg._procStop();

    var W = svg.viewBox.baseVal.width, H = svg.viewBox.baseVal.height;
    var cx = W / 2, cy = H / 2;

    var nodes = [], byType = {};
    svg.querySelectorAll(".proc-node").forEach(function (g) {
      var orb = g.querySelector(".proc-orb");
      var ox = parseFloat(orb.getAttribute("cx"));
      var oy = parseFloat(orb.getAttribute("cy"));
      var n = {
        el: g, type: g.getAttribute("data-type"), task: g.getAttribute("data-task"),
        ox: ox, oy: oy, x: ox, y: oy, vx: 0, vy: 0,
        r: parseFloat(orb.getAttribute("r")), fixed: false,
      };
      nodes.push(n);
      byType[n.type] = n;
    });
    if (!nodes.length) return;

    var edges = [];
    svg.querySelectorAll(".proc-edge").forEach(function (p) {
      var f = byType[p.getAttribute("data-from")];
      var t = byType[p.getAttribute("data-to")];
      if (f && t) edges.push({ from: f, to: t, path: p, label: p.nextElementSibling });
    });

    var groups = [];
    svg.querySelectorAll(".proc-group").forEach(function (gp) {
      var task = gp.getAttribute("data-task");
      var members = nodes.filter(function (n) { return n.task === task; });
      groups.push({ rect: gp.querySelector("rect"), label: gp.querySelector("text"), members: members });
    });

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
          var ux = dx / d, uy = dy / d;
          var rep = REP / d2;
          fx[i] += ux * rep; fy[i] += uy * rep;
          fx[j] -= ux * rep; fy[j] -= uy * rep;
          var minD = nodes[i].r + nodes[j].r + COLPAD;
          if (d < minD) {
            var push = (minD - d) * COL;
            fx[i] += ux * push; fy[i] += uy * push;
            fx[j] -= ux * push; fy[j] -= uy * push;
          }
        }
      }
      for (var e = 0; e < edges.length; e++) {
        var a = edges[e].from, b = edges[e].to;
        var ex = b.x - a.x, ey = b.y - a.y, ed = Math.sqrt(ex * ex + ey * ey) || 0.01;
        var f = (ed - LEN) * SPRING, uex = ex / ed, uey = ey / ed;
        var ai = nodes.indexOf(a), bi = nodes.indexOf(b);
        fx[ai] += uex * f; fy[ai] += uey * f;
        fx[bi] -= uex * f; fy[bi] -= uey * f;
      }
      for (var k = 0; k < nodes.length; k++) {
        var n = nodes[k];
        fx[k] += (cx - n.x) * CENTER; fy[k] += (cy - n.y) * CENTER;
        if (n.fixed) { n.vx = 0; n.vy = 0; continue; }
        n.vx = (n.vx + fx[k] * alpha) * DAMP;
        n.vy = (n.vy + fy[k] * alpha) * DAMP;
        n.vx = Math.max(-VMAX, Math.min(VMAX, n.vx));
        n.vy = Math.max(-VMAX, Math.min(VMAX, n.vy));
        n.x = Math.max(n.r, Math.min(W - n.r, n.x + n.vx));
        n.y = Math.max(n.r, Math.min(H - n.r, n.y + n.vy));
      }
      alpha *= COOL;
    }

    function loop() {
      step();
      render();
      if (alpha > 0.02) raf = requestAnimationFrame(loop);
      else raf = 0;
    }
    function reheat() { alpha = Math.max(alpha, 0.6); if (!raf) raf = requestAnimationFrame(loop); }

    // Dragging.
    function toSvg(evt) {
      var pt = svg.createSVGPoint();
      pt.x = evt.clientX; pt.y = evt.clientY;
      return pt.matrixTransform(svg.getScreenCTM().inverse());
    }
    var dragging = null;
    nodes.forEach(function (n) {
      n.el.style.cursor = "grab";
      n.el.addEventListener("pointerdown", function (evt) {
        evt.preventDefault();
        dragging = n; n.fixed = true; n.el.style.cursor = "grabbing";
        n.el.setPointerCapture(evt.pointerId);
        reheat();
      });
      n.el.addEventListener("pointermove", function (evt) {
        if (dragging !== n) return;
        var p = toSvg(evt);
        n.x = p.x; n.y = p.y; n.vx = 0; n.vy = 0;
        reheat();
      });
      n.el.addEventListener("pointerup", function (evt) {
        if (dragging !== n) return;
        dragging = null; n.fixed = false; n.el.style.cursor = "grab";
        n.el.releasePointerCapture(evt.pointerId);
        reheat();
      });
    });

    svg._procStop = function () { if (raf) cancelAnimationFrame(raf); raf = 0; };
    reheat();
  }

  function initAll() { document.querySelectorAll(".proc-graph").forEach(init); }

  document.addEventListener("htmx:afterSettle", function (e) {
    if (e.target && e.target.querySelector && e.target.querySelector(".proc-graph")) initAll();
  });
  if (document.readyState !== "loading") initAll();
  else document.addEventListener("DOMContentLoaded", initAll);

  window.ClioProcess = { init: initAll };
})();
