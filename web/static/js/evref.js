// evref.js — clickable foreign-key values in event payloads. A payload field
// like "employeeId": "E-000005" is rendered server-side (see prettyJSON in
// internal/server/inspector.go) as an <a class="ev-ref"> carrying the referenced
// subject; clicking it opens the inspector scoped to that subject's events — the
// same view the subject legend opens. Works for the hover card and the inspector
// list alike, so it listens once at the document level.
//
// Progressive enhancement, vanilla, embedded — no CDN (docs/WORKBENCH.md §2).
(function () {
  "use strict";

  function openSubject(subject) {
    var insp = document.getElementById("inspector");
    if (!insp || !window.htmx || !subject) return;
    insp.classList.add("open");
    window.htmx.ajax("GET", "/node-events?subject=" + encodeURIComponent(subject),
      { target: "#inspector", swap: "innerHTML" });
  }

  function refOf(target) {
    return target && target.closest ? target.closest(".ev-ref[data-subject]") : null;
  }

  document.addEventListener("click", function (e) {
    var a = refOf(e.target);
    if (!a) return;
    e.preventDefault();
    openSubject(a.getAttribute("data-subject"));
  });

  // Keyboard activation: ev-ref carries tabindex="0" so it can be focused.
  document.addEventListener("keydown", function (e) {
    if (e.key !== "Enter" && e.key !== " ") return;
    var a = refOf(e.target);
    if (!a) return;
    e.preventDefault();
    openSubject(a.getAttribute("data-subject"));
  });
})();
