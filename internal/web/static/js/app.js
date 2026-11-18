// All page behaviour lives in this file: the CSP allows no inline script.
"use strict";

// The toast: a success fades after five seconds; an error stays. The ×
// closes either.
(function () {
  var toast = document.getElementById("vq-toast");
  if (!toast) {
    return;
  }
  function close() {
    toast.classList.add("opacity-0");
    setTimeout(function () { toast.remove(); }, 600);
  }
  var btn = toast.querySelector("[data-toast-close]");
  if (btn) {
    btn.addEventListener("click", close);
  }
  if (!toast.hasAttribute("data-sticky")) {
    setTimeout(close, 5000);
  }
})();
