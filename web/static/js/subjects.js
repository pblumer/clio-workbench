// subjects.js — a colour-coded, clickable legend of the subjects (streams) in
// the discovered process. Each subject gets the same vivid hue the graph uses
// to tint its sequence flows during replay, so you can read "which colour is
// which stream" at a glance. Clicking a subject opens the inspector with all of
// that subject's events.
//
// Progressive enhancement, vanilla, embedded — no CDN (docs/WORKBENCH.md §2).
// It reads the same .proc-replay payload process.js does and deliberately keeps
// streamColor() byte-for-byte in sync with the one in process.js.
(function () {
  "use strict";

  // streamColor maps a subject to a stable, vivid hue. MUST match process.js.
  function streamColor(s) {
    var h = 0;
    for (var i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) % 360;
    return "hsl(" + h + ", 90%, 64%)";
  }

  function openSubject(subject) {
    var insp = document.getElementById("inspector");
    if (!insp || !window.htmx) return;
    insp.classList.add("open");
    window.htmx.ajax("GET", "/node-events?subject=" + encodeURIComponent(subject),
      { target: "#inspector", swap: "innerHTML" });
  }

  // focusSubject tells the process graph (process.js) to spotlight one stream's
  // path; passing null clears the focus.
  function focusSubject(subject) {
    document.dispatchEvent(new CustomEvent("clio:focus-subject", { detail: { s: subject } }));
  }

  function init(container) {
    if (container._subjInit) return;
    container._subjInit = true;

    // The replay payload lives in the sibling .proc-graph; fall back to the
    // whole panel if the markup nests differently.
    var scope = container.parentElement || document;
    var scriptEl = scope.querySelector(".proc-replay");
    if (!scriptEl) { container.style.display = "none"; return; }

    var replay = [];
    try { replay = JSON.parse(scriptEl.textContent || "[]"); } catch (e) { replay = []; }
    if (!replay.length) { container.style.display = "none"; return; }

    // Count events per subject, preserving first-seen order for stable ties.
    var counts = {}, order = [];
    for (var i = 0; i < replay.length; i++) {
      var s = replay[i].s;
      if (!(s in counts)) { counts[s] = 0; order.push(s); }
      counts[s]++;
    }
    order.sort(function (a, b) { return counts[b] - counts[a] || (a < b ? -1 : 1); });

    var total = order.length;
    var activeChip = null; // the chip currently focusing the graph, if any
    var list = document.createElement("div");
    list.className = "subj-list";
    order.forEach(function (s) {
      var chip = document.createElement("button");
      chip.type = "button";
      chip.className = "subj-chip";
      chip.style.color = streamColor(s);
      chip.title = s + " — " + counts[s] + " events";
      var dot = document.createElement("span");
      dot.className = "subj-dot";
      dot.style.background = streamColor(s);
      var name = document.createElement("span");
      name.className = "subj-name";
      name.textContent = s;
      var n = document.createElement("span");
      n.className = "subj-n";
      n.textContent = counts[s];
      chip.appendChild(dot);
      chip.appendChild(name);
      chip.appendChild(n);
      // A click always opens this subject's events in the inspector and focuses
      // the graph on its path; clicking the focused chip again clears the focus.
      chip.addEventListener("click", function () {
        openSubject(s);
        if (activeChip === chip) {
          chip.classList.remove("active");
          activeChip = null;
          focusSubject(null);
          return;
        }
        if (activeChip) activeChip.classList.remove("active");
        activeChip = chip;
        chip.classList.add("active");
        focusSubject(s);
      });
      list.appendChild(chip);
    });

    var det = document.createElement("details");
    det.className = "subj-box";
    if (total <= 12) det.open = true;
    var sum = document.createElement("summary");
    sum.className = "subj-summary";
    sum.textContent = "Subjects · " + total;
    det.appendChild(sum);
    det.appendChild(list);

    container.textContent = "";
    container.appendChild(det);
    container.style.display = "";
  }

  function initAll() { document.querySelectorAll(".proc-subjects").forEach(init); }

  document.addEventListener("htmx:afterSettle", function (e) {
    if (e.target && e.target.querySelector && e.target.querySelector(".proc-subjects")) initAll();
  });
  if (document.readyState !== "loading") initAll();
  else document.addEventListener("DOMContentLoaded", initAll);

  window.ClioSubjects = { init: initAll };
})();
