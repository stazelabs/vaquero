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
  if (!document.querySelector("[data-live], [data-live-mcp]") || !window.EventSource || !window.htmx) {
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
  // MCP calls refresh only the parts that follow them (the MCP activity page).
  function refreshMCP() {
    document.querySelectorAll("[data-live-mcp]").forEach(function (el) {
      window.htmx.trigger(el, "vq:mcp");
    });
  }
  function open() {
    if (source) {
      return;
    }
    source = new EventSource("/events/stream");
    source.addEventListener("change", refresh);
    source.addEventListener("mcp", refreshMCP);
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
      refreshMCP();
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
// g b board, g l list, j/k next/previous card (enter on its focused link
// opens it), ? shortcuts.
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
  // Visible cards only: a collapsed rail's cards are skipped.
  function cards() {
    return Array.prototype.slice.call(document.querySelectorAll("[data-card]")).filter(function (el) {
      // checkVisibility sees a closed <details>; getClientRects is the fallback.
      return el.checkVisibility ? el.checkVisibility() : el.getClientRects().length > 0;
    });
  }
  // j and k move real focus to the next or previous card's link, starting
  // from the card that holds focus (after Tab or a click, too). The card
  // draws its ring while its link is focused (focus-within in its class).
  function focusCard(step) {
    var list = cards();
    if (!list.length) {
      return;
    }
    var active = document.activeElement;
    var at = active && active.closest ? list.indexOf(active.closest("[data-card]")) : -1;
    var next = at < 0 ? 0 : Math.max(0, Math.min(list.length - 1, at + step));
    var link = list[next].querySelector("a");
    if (link) {
      link.focus({ preventScroll: true });
    }
    list[next].scrollIntoView({ block: "nearest", inline: "nearest" });
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
  // A board or list refresh replaces the cards: the focused card's link is
  // focused again in the new markup, so j and k carry on from it.
  var refocus = "";
  document.body.addEventListener("htmx:beforeSwap", function () {
    var active = document.activeElement;
    var card = active && active.closest ? active.closest("[data-card]") : null;
    refocus = card ? card.dataset.card : "";
  });
  document.body.addEventListener("htmx:afterSettle", function () {
    if (!refocus) {
      return;
    }
    var card = document.querySelector('[data-card="' + refocus + '"]');
    var link = card ? card.querySelector("a") : null;
    refocus = "";
    if (link && (document.activeElement === document.body || !document.activeElement)) {
      link.focus({ preventScroll: true });
    }
  });
})();

// Claim countdowns: every [data-claim-expires] (a lease's end, an ISO
// instant) reads "12 min left", rewritten every 30 seconds and after each
// swap. A lease that has run out reads "lapsing" until the sweep returns
// the ticket and the live refresh redraws the card.
(function () {
  function words(ms) {
    if (ms <= 0) { return "lapsing"; }
    var min = Math.ceil(ms / 60000);
    if (min < 60) { return min + " min left"; }
    var h = Math.floor(min / 60), m = min % 60;
    return h + " h " + (m ? m + " min " : "") + "left";
  }
  function tick() {
    var now = Date.now();
    document.querySelectorAll("[data-claim-expires]").forEach(function (el) {
      var at = Date.parse(el.getAttribute("data-claim-expires"));
      if (!isNaN(at)) { el.textContent = words(at - now); }
    });
  }
  tick();
  setInterval(tick, 30000);
  document.body.addEventListener("htmx:afterSettle", tick);
})();

// Attachments: paste or drop files on a [data-attach-url] text box, or drop
// or choose them on the well, and each is uploaded to that address. In a
// text box the file's place is held by "Uploading {name}…" at the cursor,
// then swapped for the Markdown the server answers with (or removed, and
// the refusal shown as a toast). Handlers sit on the document so they
// survive the live refresh that replaces the attachments section.
(function () {
  var uploadCount = 0;
  function toast(message) {
    var tpl = document.getElementById("vq-toast-template");
    if (!tpl) { return; }
    var old = document.getElementById("vq-toast");
    if (old) { old.remove(); }
    var el = tpl.content.firstElementChild.cloneNode(true);
    el.querySelector("[data-toast-message]").textContent = message;
    var btn = el.querySelector("[data-toast-close]");
    if (btn) {
      btn.addEventListener("click", function () { el.remove(); });
    }
    document.body.appendChild(el);
  }

  function hasFiles(ev) {
    var dt = ev.dataTransfer;
    return !!dt && Array.prototype.indexOf.call(dt.types || [], "Files") >= 0;
  }

  // The placeholder goes at the cursor, and the cursor moves past it.
  function insertAtCursor(box, text) {
    var start = box.selectionStart, end = box.selectionEnd;
    if (typeof start !== "number") { start = end = box.value.length; }
    // Each file starts its own line; the break stays outside the placeholder.
    var lead = start > 0 && box.value.charAt(start - 1) !== "\n" ? "\n" : "";
    box.value = box.value.slice(0, start) + lead + text + box.value.slice(end);
    box.selectionStart = box.selectionEnd = start + lead.length + text.length;
  }

  function swapPlaceholder(box, placeholder, text) {
    var at = box.value.indexOf(placeholder);
    if (at < 0) {
      // The line was deleted mid-upload; keep the result rather than lose it.
      if (text) {
        box.value += (box.value === "" || box.value.slice(-1) === "\n" ? "" : "\n") + text;
      }
      return;
    }
    var cursor = box.selectionStart;
    box.value = box.value.slice(0, at) + text + box.value.slice(at + placeholder.length);
    var delta = text.length - placeholder.length;
    if (cursor > at) { cursor += delta; }
    box.selectionStart = box.selectionEnd = cursor;
  }

  function refreshList() {
    if (!window.htmx) { return; }
    document.querySelectorAll("#attachments[data-live]").forEach(function (el) {
      window.htmx.trigger(el, "vq:change");
    });
  }

  function upload(url, max, file, box) {
    var name = file.name || "file";
    if (max > 0 && file.size > max) {
      toast("Files can be at most " + Math.round(max / 1048576) + " MB.");
      return Promise.resolve();
    }
    uploadCount += 1;
    var placeholder = "Uploading " + name + " (" + uploadCount + ")…";
    if (box) { insertAtCursor(box, placeholder); }
    var form = new FormData();
    form.append("file", file, name);
    return fetch(url, { method: "POST", body: form, credentials: "same-origin" })
      .then(function (res) {
        return res.json().catch(function () { return {}; }).then(function (body) {
          if (res.ok && body.markdown) {
            if (box) { swapPlaceholder(box, placeholder, body.markdown); }
            return;
          }
          if (box) { swapPlaceholder(box, placeholder, ""); }
          toast((body.error && body.error.message) || "The upload failed. Try again.");
        });
      })
      .catch(function () {
        if (box) { swapPlaceholder(box, placeholder, ""); }
        toast("The upload failed. Try again.");
      });
  }

  function uploadAll(target, files, box) {
    var url = target.getAttribute("data-attach-url");
    var max = parseInt(target.getAttribute("data-max-bytes") || "0", 10);
    var list = Array.prototype.slice.call(files);
    // One at a time, so the placeholders land in the order the files came.
    list.reduce(function (p, f) {
      return p.then(function () { return upload(url, max, f, box); });
    }, Promise.resolve()).then(refreshList);
  }

  function boxOf(ev) {
    return ev.target.closest ? ev.target.closest("textarea[data-attach-url]") : null;
  }
  function wellOf(ev) {
    return ev.target.closest ? ev.target.closest("[data-attach-well]") : null;
  }

  document.addEventListener("paste", function (ev) {
    var box = boxOf(ev);
    var files = ev.clipboardData && ev.clipboardData.files;
    if (!box || !files || files.length === 0) { return; }
    // A paste that carries text (a copied document or cell) keeps its
    // default. An image copied from a page or an app carries HTML but no
    // text, and the browser hides a copied file's name, so both upload.
    if ((ev.clipboardData.getData("text/plain") || "").trim()) { return; }
    ev.preventDefault();
    uploadAll(box, files, box);
  });

  document.addEventListener("dragover", function (ev) {
    var t = boxOf(ev) || wellOf(ev);
    if (!t || !hasFiles(ev)) { return; }
    ev.preventDefault();
    var well = wellOf(ev);
    if (well) { well.setAttribute("data-over", ""); }
  });
  document.addEventListener("dragleave", function (ev) {
    var well = wellOf(ev);
    if (well && !well.contains(ev.relatedTarget)) { well.removeAttribute("data-over"); }
  });
  document.addEventListener("drop", function (ev) {
    var box = boxOf(ev), well = wellOf(ev);
    if (well) { well.removeAttribute("data-over"); }
    if (!(box || well) || !hasFiles(ev)) { return; }
    ev.preventDefault();
    uploadAll(box || well, ev.dataTransfer.files, box);
  });

  document.addEventListener("click", function (ev) {
    var btn = ev.target.closest ? ev.target.closest("[data-attach-choose]") : null;
    if (!btn) { return; }
    var well = btn.closest("[data-attach-well]");
    var input = well && well.querySelector("[data-attach-input]");
    if (input) { input.click(); }
  });
  document.addEventListener("change", function (ev) {
    var input = ev.target;
    if (!input.matches || !input.matches("[data-attach-input]")) { return; }
    var well = input.closest("[data-attach-well]");
    if (well && input.files.length > 0) { uploadAll(well, input.files, null); }
    input.value = "";
  });
})();
