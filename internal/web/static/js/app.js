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

// The board: every [data-board-column] list is a Sortable in one group, so
// cards drag within and between columns. A drop posts the card's new
// status and neighbours (by ticket key) through htmx, which swaps in the
// board the server renders back. window.vqDragging is true mid-drag, so a
// live refresh can wait (see the stream handler).
window.vqDragging = false;
(function () {
  function key(el) {
    return el && el.dataset ? el.dataset.card || "" : "";
  }
  function init(root) {
    if (!window.Sortable || !window.htmx) {
      return;
    }
    root.querySelectorAll("[data-board-column]").forEach(function (list) {
      if (list.dataset.sortable) {
        return;
      }
      list.dataset.sortable = "1";
      window.Sortable.create(list, {
        group: "board",
        animation: 150,
        // Pointer-event dragging rather than native HTML5 drag and drop:
        // consistent across browsers and touch, and drivable in tests.
        forceFallback: true,
        draggable: "[data-card]",
        onStart: function () {
          window.vqDragging = true;
        },
        onEnd: function (ev) {
          window.vqDragging = false;
          if (ev.from === ev.to && ev.oldIndex === ev.newIndex) {
            document.dispatchEvent(new CustomEvent("vq:dragend"));
            return;
          }
          var board = document.getElementById("board");
          window.htmx.ajax("POST", "/t/" + key(ev.item) + "/move", {
            target: "#board",
            swap: "outerHTML",
            values: {
              status: ev.to.dataset.status,
              before: key(ev.item.previousElementSibling),
              after: key(ev.item.nextElementSibling),
              filter: board ? board.dataset.filter : "",
            },
          });
          document.dispatchEvent(new CustomEvent("vq:dragend"));
        },
      });
    });
  }
  init(document);
  document.body.addEventListener("htmx:afterSettle", function () {
    init(document);
  });
})();

// Live updates. One EventSource per tab (not per page part) listens to
// /events/stream; on "change" every [data-live] element gets a vq:change
// event, which its hx-trigger turns into a refresh. A hidden tab closes its
// stream, since browsers allow only six connections to one host, and
// refreshes once when it is shown again. A refresh of #board waits while a
// card is being dragged.
(function () {
  if (!document.querySelector("[data-live]") || !window.EventSource || !window.htmx) {
    return;
  }
  var source = null;
  var pending = false;
  function refresh() {
    document.querySelectorAll("[data-live]").forEach(function (el) {
      if (el.id === "board" && window.vqDragging) {
        pending = true;
        return;
      }
      window.htmx.trigger(el, "vq:change");
    });
  }
  function open() {
    if (source) {
      return;
    }
    source = new EventSource("/events/stream");
    source.addEventListener("change", refresh);
    window.vqStream = source;
  }
  function close() {
    if (source) {
      source.close();
      source = null;
      window.vqStream = null;
    }
  }
  document.addEventListener("visibilitychange", function () {
    if (document.hidden) {
      close();
    } else {
      open();
      refresh(); // catch up on anything missed while hidden
    }
  });
  window.addEventListener("pagehide", close);
  document.addEventListener("vq:dragend", function () {
    if (pending) {
      pending = false;
      refresh();
    }
  });
  if (!document.hidden) {
    open();
  }
})();

// A board refresh replaces #board: rails that were open stay open and the
// columns keep their sideways scroll.
(function () {
  var openRails = null;
  var scroll = 0;
  document.body.addEventListener("htmx:beforeSwap", function (ev) {
    var target = ev.detail && ev.detail.target;
    if (!target || target.id !== "board") {
      return;
    }
    openRails = Array.prototype.map.call(target.querySelectorAll("details[data-rail][open]"), function (d) {
      return d.dataset.rail;
    });
    var cols = target.querySelector("[data-board-scroll]");
    scroll = cols ? cols.scrollLeft : 0;
  });
  document.body.addEventListener("htmx:afterSettle", function () {
    var board = document.getElementById("board");
    if (!board || openRails === null) {
      return;
    }
    openRails.forEach(function (id) {
      var d = board.querySelector('details[data-rail="' + id + '"]');
      if (d) {
        d.open = true;
      }
    });
    var cols = board.querySelector("[data-board-scroll]");
    if (cols) {
      cols.scrollLeft = scroll;
    }
    openRails = null;
  });
})();

// Keyboard shortcuts (listed in the ? dialog): c new ticket, / search,
// g b board, g l list, j/k next/previous card, enter open, ? shortcuts.
// Ignored while typing in a field.
(function () {
  var dialog = document.getElementById("vq-shortcuts");
  var opener = document.querySelector("[data-shortcuts-open]");
  if (opener && dialog) {
    opener.addEventListener("click", function () { dialog.showModal(); });
  }
  function project() {
    var el = document.querySelector("[data-project]");
    return el ? el.dataset.project : "";
  }
  function typing(el) {
    if (!el) {
      return false;
    }
    var tag = el.tagName;
    return tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || el.isContentEditable;
  }
  var current = -1;
  // Visible cards only: a collapsed rail's cards are skipped.
  function cards() {
    return Array.prototype.slice.call(document.querySelectorAll("[data-card]")).filter(function (el) {
      // checkVisibility sees a closed <details>; getClientRects is the fallback.
      return el.checkVisibility ? el.checkVisibility() : el.getClientRects().length > 0;
    });
  }
  function focusCard(step) {
    var list = cards();
    if (!list.length) {
      return;
    }
    if (current >= 0 && list[current]) {
      list[current].classList.remove("ring-2", "ring-accent");
    }
    current = Math.max(0, Math.min(list.length - 1, current + step));
    var el = list[current];
    el.classList.add("ring-2", "ring-accent");
    el.scrollIntoView({ block: "nearest", inline: "nearest" });
  }
  var pendingG = false;
  document.addEventListener("keydown", function (ev) {
    if (ev.metaKey || ev.ctrlKey || ev.altKey || typing(ev.target)) {
      return;
    }
    if (dialog && dialog.open) {
      return; // the dialog closes itself on esc
    }
    var k = ev.key;
    var p = project();
    if (pendingG) {
      pendingG = false;
      if (k === "b") {
        window.location.href = p ? "/p/" + p : "/";
      } else if (k === "l") {
        window.location.href = p ? "/p/" + p + "/list" : "/";
      }
      ev.preventDefault();
      return;
    }
    switch (k) {
      case "g":
        pendingG = true;
        setTimeout(function () { pendingG = false; }, 1500);
        break;
      case "c":
        window.location.href = p ? "/p/" + p + "/new" : "/projects/new";
        break;
      case "/":
        var box = document.getElementById("vq-search");
        if (box) {
          box.focus();
        }
        break;
      case "j":
        focusCard(1);
        break;
      case "k":
        focusCard(-1);
        break;
      case "Enter":
        // Enter on a focused link, button or rail summary is the browser's.
        if (ev.target.closest && ev.target.closest("a,button,summary,[tabindex]")) {
          return;
        }
        var list = cards();
        var link = current >= 0 && list[current] ? list[current].querySelector("a") : null;
        if (!link) {
          return;
        }
        link.click();
        break;
      case "?":
        if (dialog) {
          dialog.showModal();
        }
        break;
      default:
        return;
    }
    ev.preventDefault();
  });
  // A board refresh replaces the cards; start the focus over.
  document.body.addEventListener("htmx:afterSwap", function () { current = -1; });
})();
