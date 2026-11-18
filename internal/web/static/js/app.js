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

// Live refreshes wait while a card is being dragged: a refresh of #board
// asked for mid-drag is dropped and run once the drag ends.
(function () {
  var pending = false;
  document.body.addEventListener("htmx:beforeRequest", function (ev) {
    var elt = ev.detail && ev.detail.elt;
    if (window.vqDragging && elt && elt.id === "board" && ev.detail.requestConfig && ev.detail.requestConfig.verb === "get") {
      ev.preventDefault();
      pending = true;
    }
  });
  document.addEventListener("vq:dragend", function () {
    if (pending && window.htmx) {
      pending = false;
      var board = document.getElementById("board");
      if (board) {
        window.htmx.trigger(board, "sse:change");
      }
    }
  });
})();
