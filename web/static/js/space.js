// space.js — turns the event-type cards into a living "event space": the cards
// orbit a glowing core (the total) and are tethered to it by neon filaments.
//
// Progressive enhancement: without JS the cards fall back to a simple grid
// (see .event-space:not(.is-space) in workbench.css). Pure vanilla, embedded
// via embed.FS — no framework, no CDN (docs/WORKBENCH.md §2).
(function () {
  "use strict";

  // arrange positions every card on a ring around the centre and draws the
  // filaments. Card bobbing itself is CSS; the filament anchors stay fixed at
  // the ring point so the cards look gently tethered.
  function arrange(space) {
    var cards = Array.prototype.slice.call(space.querySelectorAll(".bpmn-card"));
    if (!cards.length) return;

    var w = space.clientWidth;
    var h = space.clientHeight;
    if (w === 0 || h === 0) return;

    var cx = w / 2;
    var cy = h / 2;
    var n = cards.length;
    var base = Math.min(w, h) * 0.34;

    cards.forEach(function (card, i) {
      var ang = (i / n) * Math.PI * 2 - Math.PI / 2;
      // Vary the radius a little so nodes sit at different "depths".
      var r = base * (0.82 + 0.18 * Math.sin(i * 1.7));
      var x = cx + Math.cos(ang) * r;
      var y = cy + Math.sin(ang) * r;
      card.style.left = x + "px";
      card.style.top = y + "px";
      card.style.setProperty("--bob-delay", (i * 0.37).toFixed(2) + "s");
      card.style.setProperty("--bob-dur", (5.5 + (i % 4) * 0.8).toFixed(2) + "s");
      card._cx = x;
      card._cy = y;
    });

    drawLinks(space, cards, cx, cy, w, h);
    space.classList.add("is-space");
  }

  function drawLinks(space, cards, cx, cy, w, h) {
    var svg = space.querySelector(".space-links");
    if (!svg) return;
    svg.setAttribute("viewBox", "0 0 " + w + " " + h);
    var parts = cards.map(function (card) {
      return (
        '<line class="space-link" x1="' + cx + '" y1="' + cy +
        '" x2="' + card._cx + '" y2="' + card._cy + '"/>'
      );
    });
    svg.innerHTML = parts.join("");
  }

  function arrangeAll() {
    document.querySelectorAll(".event-space").forEach(arrange);
  }

  function debounce(fn, ms) {
    var t;
    return function () {
      clearTimeout(t);
      t = setTimeout(fn, ms);
    };
  }

  // Re-arrange whenever the events fragment is (re)loaded via HTMX.
  document.addEventListener("htmx:afterSettle", function (e) {
    if (e.target && e.target.querySelector && e.target.querySelector(".event-space")) {
      arrangeAll();
    }
  });
  window.addEventListener("resize", debounce(arrangeAll, 150));
  if (document.readyState !== "loading") arrangeAll();
  else document.addEventListener("DOMContentLoaded", arrangeAll);

  window.ClioSpace = { arrange: arrangeAll };
})();
