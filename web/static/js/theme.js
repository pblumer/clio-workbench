// theme.js — applies and persists the colour theme (docs/THEMES.md).
//
// The chosen theme is not a secret (only the Clio token must stay server-side,
// principle 4), so the cookie is written here on the client. The server reads
// it back on the next request to render <html data-theme="…"> before paint, so
// switching is instant and reloads never flash the wrong palette.
(function () {
  "use strict";

  function apply(id) {
    document.documentElement.setAttribute("data-theme", id);
  }

  // Called by the switcher's <select onchange>.
  window.setTheme = function (id) {
    apply(id);
    document.cookie =
      "wb-theme=" + encodeURIComponent(id) +
      ";path=/;max-age=31536000;samesite=lax"; // ~1 year, cosmetic only
  };

  // Safety net: if a page rendered without a server-set data-theme, adopt the
  // cookie's value before paint so every page stays in sync.
  if (!document.documentElement.getAttribute("data-theme")) {
    var m = document.cookie.match(/(?:^|;\s*)wb-theme=([^;]+)/);
    if (m) apply(decodeURIComponent(m[1]));
  }
})();
