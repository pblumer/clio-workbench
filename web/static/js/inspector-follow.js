// inspector-follow.js — during replay, scroll the open event-list inspector to
// each event as it flashes on the process graph, and briefly highlight it.
//
// process.js emits a "clio:replay-step" event per replay step ({s,t,ts}); when
// the inspector is open and "follow" is on, we find the matching .ev-item and
// bring it into view. The toggle (a checkbox in the inspector header) is simple
// and its state persists in localStorage. Vanilla, embedded, no CDN.
(function () {
  "use strict";

  var KEY = "clio.followReplay";

  function enabled() {
    // Default on — the effect is the point; off is opt-out.
    return localStorage.getItem(KEY) !== "0";
  }
  function setEnabled(on) { localStorage.setItem(KEY, on ? "1" : "0"); }

  // wireToggle syncs the header checkbox with the stored preference. Called on
  // every inspector load (htmx swaps in a fresh checkbox each time).
  function wireToggle() {
    var cb = document.querySelector("#inspector .insp-follow-cb");
    if (!cb || cb._followWired) return;
    cb._followWired = true;
    cb.checked = enabled();
    cb.addEventListener("change", function () { setEnabled(cb.checked); });
  }

  function esc(s) {
    if (window.CSS && CSS.escape) return CSS.escape(s);
    return String(s).replace(/["\\]/g, "\\$&");
  }

  // Scroll the item into the centre of its scroll container without moving the
  // whole page.
  function centerInList(item) {
    var list = item.closest(".ev-list");
    if (!list) { item.scrollIntoView({ block: "nearest" }); return; }
    var lr = list.getBoundingClientRect(), ir = item.getBoundingClientRect();
    if (ir.top < lr.top || ir.bottom > lr.bottom) {
      list.scrollTop += (ir.top - lr.top) - (lr.height - ir.height) / 2;
    }
  }

  var lastFocused = null;
  function focusItem(item) {
    if (lastFocused && lastFocused !== item) lastFocused.classList.remove("ev-focus");
    centerInList(item);
    item.classList.remove("ev-focus");
    // reflow so re-adding the class restarts the highlight animation
    void item.offsetWidth;
    item.classList.add("ev-focus");
    lastFocused = item;
  }

  document.addEventListener("clio:replay-step", function (e) {
    if (!enabled()) return;
    var insp = document.getElementById("inspector");
    if (!insp || !insp.classList.contains("open")) return;
    var d = e.detail || {};
    if (!d.ts) return;
    var item;
    try {
      item = insp.querySelector(
        '.ev-item[data-subject="' + esc(d.s) + '"]' +
        '[data-type="' + esc(d.t) + '"]' +
        '[data-time="' + esc(d.ts) + '"]');
    } catch (err) { item = null; }
    if (item) focusItem(item);
  });

  document.addEventListener("htmx:afterSettle", function (e) {
    if (e.target && e.target.id === "inspector") wireToggle();
  });
  if (document.readyState !== "loading") wireToggle();
  else document.addEventListener("DOMContentLoaded", wireToggle);
})();
