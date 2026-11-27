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

// A page that answers a POST it must never repeat (the one showing a new
// API token) carries data-replace-url: its history entry becomes a GET of
// that address, so a refresh or a return to it doesn't offer to resubmit.
(function () {
  var el = document.querySelector("[data-replace-url]");
  if (el && window.history && window.history.replaceState) {
    window.history.replaceState(null, "", el.getAttribute("data-replace-url"));
  }
})();

// vqToast shows an error toast for a failure the page sees itself (an
// upload, a board drop), cloned from the Shell's #vq-toast-template.
window.vqToast = function (message) {
  var tpl = document.getElementById("vq-toast-template");
  if (!tpl) {
    return;
  }
  var old = document.getElementById("vq-toast");
  if (old) {
    old.remove();
  }
  var el = tpl.content.firstElementChild.cloneNode(true);
  el.querySelector("[data-toast-message]").textContent = message;
  var btn = el.querySelector("[data-toast-close]");
  if (btn) {
    btn.addEventListener("click", function () { el.remove(); });
  }
  document.body.appendChild(el);
};

// The board: every [data-board-column] list is a Sortable in one group, so
// cards drag within and between columns. A drop posts the card's new
// status and neighbours (by ticket key) through htmx, which swaps in the
// board the server renders back. window.vqDragging is true mid-drag, so a
// live refresh can wait (see the stream handler), and so does a drop's
// answer: a board that arrives mid-drag isn't swapped in under the card;
// when that drag ends without a move the board is fetched afresh, and when
// it ends in a move that drop's answer is the latest. A drop that fails
// (an error answer, or no answer) puts its own card back, says so and
// fetches the board again. Each drop's request carries a number in a
// Vaquero-Drop header, and where its card came from is remembered under that
// number, so when two drops overlap the failed one's card is the one put
// back.
window.vqDragging = false;
(function () {
  function key(el) {
    return el && el.dataset ? el.dataset.card || "" : "";
  }
  var deferred = false; // a board answer was held back mid-drag
  var drops = {}; // drop number → {item, from, index}: where its card came from
  var dropSeq = 0;
  function dropOf(ev) {
    var cfg = ev.detail && ev.detail.requestConfig;
    var n = cfg && cfg.headers ? Number(cfg.headers["Vaquero-Drop"]) : NaN;
    return isNaN(n) ? 0 : n;
  }
  function refreshBoard() {
    var board = document.getElementById("board");
    if (board && window.htmx) {
      window.htmx.trigger(board, "vq:change");
    }
  }
  function isDrop(ev) {
    var cfg = ev.detail && ev.detail.requestConfig;
    return !!cfg && cfg.verb === "post" && /^\/t\/[^/]+\/move$/.test(cfg.path || "");
  }
  document.body.addEventListener("htmx:beforeSwap", function (ev) {
    var target = ev.detail && ev.detail.target;
    if (target && target.id === "board" && window.vqDragging && ev.detail.shouldSwap) {
      ev.detail.shouldSwap = false;
      deferred = true;
    }
  });
  function dropFailed(ev) {
    if (!isDrop(ev)) {
      return;
    }
    var n = dropOf(ev);
    var d = drops[n];
    delete drops[n];
    if (d && d.item.isConnected && !window.vqDragging) {
      // Out first, so the index counts the list as it was before the drag
      // (an upward move in one column would otherwise land one too low).
      d.item.remove();
      d.from.insertBefore(d.item, d.from.children[d.index] || null);
    }
    window.vqToast("The card couldn't be moved. Try again.");
    refreshBoard();
  }
  document.body.addEventListener("htmx:responseError", dropFailed);
  document.body.addEventListener("htmx:sendError", dropFailed);
  // A drop that succeeded answers with the whole board, so it and every
  // earlier drop (including one htmx dropped from its queue) are settled.
  document.body.addEventListener("htmx:afterRequest", function (ev) {
    if (!isDrop(ev) || !ev.detail.successful) {
      return;
    }
    var n = dropOf(ev);
    Object.keys(drops).forEach(function (k) {
      if (Number(k) <= n) {
        delete drops[k];
      }
    });
  });
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
            if (deferred) {
              deferred = false;
              refreshBoard();
            }
            document.dispatchEvent(new CustomEvent("vq:dragend"));
            return;
          }
          deferred = false; // this drop's answer will be the latest board
          dropSeq += 1;
          drops[dropSeq] = { item: ev.item, from: ev.from, index: ev.oldIndex };
          var board = document.getElementById("board");
          window.htmx.ajax("POST", "/t/" + key(ev.item) + "/move", {
            target: "#board",
            swap: "outerHTML",
            headers: { "Vaquero-Drop": String(dropSeq) },
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
// event, which its hx-trigger turns into a refresh. The stream opens from
// the event sequence the page was rendered at (the body's data-event-seq,
// sent as ?since=), and each change carries its sequence as the event id,
// so the server announces at once anything the page missed: before the
// stream connected, while the server restarted (the browser reconnects
// with Last-Event-ID) or while the tab was hidden. A hidden tab closes its
// stream, since browsers allow only six connections to one host, and
// reopens it from the last sequence it saw when shown again. A refresh of
// #board waits while a card is being dragged.
(function () {
  if (!document.querySelector("[data-live], [data-live-mcp]") || !window.EventSource || !window.htmx) {
    return;
  }
  var source = null;
  var pending = false;
  var seen = document.body.getAttribute("data-event-seq") || "";
  function refresh() {
    document.querySelectorAll("[data-live]").forEach(function (el) {
      if (el.id === "board" && window.vqDragging) {
        pending = true;
        return;
      }
      if (busy(el)) {
        el.dataset.vqDeferred = "1";
        return;
      }
      delete el.dataset.vqDeferred;
      window.htmx.trigger(el, "vq:change");
    });
  }
  // A live part is busy, and its refresh waits, while swapping it would
  // cost the owner something: a field in it has focus or holds unsent text
  // (a note being typed), a pointer is down in it (a click between press
  // and release must land on the same button), or the confirm dialog is
  // open (its form may be in the part). A busy part is flagged once
  // (data-vq-deferred), with no listener per skip; one document-level
  // check refreshes the flagged parts when they are no longer busy.
  var pointerIn = null;
  function busy(el) {
    var confirm = document.getElementById("vq-confirm");
    if (confirm && confirm.open) {
      return true;
    }
    if (pointerIn === el) {
      return true;
    }
    var active = document.activeElement;
    if (active && el.contains(active) && active.matches("input, textarea, select, [contenteditable]")) {
      return true;
    }
    var fields = el.querySelectorAll("input, textarea");
    for (var i = 0; i < fields.length; i++) {
      var f = fields[i];
      if (f.type === "checkbox" || f.type === "radio") {
        if (f.checked !== f.defaultChecked) {
          return true;
        }
      } else if (f.type !== "hidden" && f.value !== f.defaultValue) {
        return true;
      }
    }
    return false;
  }
  function recheck() {
    if (!document.hasFocus()) {
      return;
    }
    document.querySelectorAll("[data-live][data-vq-deferred]").forEach(function (el) {
      if (!busy(el)) {
        delete el.dataset.vqDeferred;
        window.htmx.trigger(el, "vq:change");
      }
    });
  }
  function recheckSoon() {
    setTimeout(recheck, 0); // after focus has moved to its new element
  }
  document.addEventListener("focusout", recheckSoon);
  document.addEventListener("input", recheckSoon);
  window.addEventListener("focus", recheckSoon);
  document.addEventListener("pointerdown", function (ev) {
    pointerIn = ev.target.closest ? ev.target.closest("[data-live]") : null;
  }, true);
  ["pointerup", "pointercancel"].forEach(function (name) {
    document.addEventListener(name, function () {
      pointerIn = null;
      recheckSoon();
    }, true);
  });
  var confirmDialog = document.getElementById("vq-confirm");
  if (confirmDialog) {
    confirmDialog.addEventListener("close", recheckSoon);
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
    source = new EventSource("/events/stream" + (seen ? "?since=" + encodeURIComponent(seen) : ""));
    source.addEventListener("change", function (ev) {
      if (ev.lastEventId) {
        seen = ev.lastEventId;
      }
      refresh();
    });
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
      // The stream reopens from the last sequence seen and announces
      // anything missed while hidden; without one, refresh to be sure.
      if (!seen) {
        refresh();
      }
      open();
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

// A board refresh replaces #board: rails that were open stay open, so does
// the milestones disclosure (below lg), and the columns keep their sideways
// scroll.
(function () {
  var openRails = null;
  var openMilestones = false;
  var scroll = 0;
  document.body.addEventListener("htmx:beforeSwap", function (ev) {
    var target = ev.detail && ev.detail.target;
    if (!target || target.id !== "board" || !ev.detail.shouldSwap) {
      return;
    }
    openRails = Array.prototype.map.call(target.querySelectorAll("details[data-rail][open]"), function (d) {
      return d.dataset.rail;
    });
    openMilestones = !!target.querySelector("details[data-milestones][open]");
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
    var ms = board.querySelector("details[data-milestones]");
    if (ms && openMilestones) {
      ms.open = true;
    }
    var cols = board.querySelector("[data-board-scroll]");
    if (cols) {
      cols.scrollLeft = scroll;
    }
    openRails = null;
  });
})();

// Confirm before a destructive post: a form carrying data-confirm="question"
// opens the Shell's #vq-confirm dialog instead of posting. Its confirm
// button takes the label of the button that submitted the form; Cancel,
// Escape or a click on the backdrop close it and nothing is posted; confirm
// submits the original form, with its original submitter. Without this
// script the form posts directly.
(function () {
  var dialog = document.getElementById("vq-confirm");
  if (!dialog) {
    return;
  }
  var message = dialog.querySelector("[data-confirm-message]");
  var ok = dialog.querySelector("[data-confirm-ok]");
  var cancel = dialog.querySelector("[data-confirm-cancel]");
  var pending = null;
  document.addEventListener("submit", function (ev) {
    var form = ev.target;
    if (!form.matches || !form.matches("form[data-confirm]")) {
      return;
    }
    if (form.dataset.confirmed === "1") {
      delete form.dataset.confirmed;
      return; // confirmed: let it post
    }
    ev.preventDefault();
    var submitter = ev.submitter || null;
    pending = { form: form, submitter: submitter };
    message.textContent = form.dataset.confirm;
    var label = submitter ? submitter.textContent.trim() : "";
    ok.textContent = label || "Confirm";
    dialog.showModal();
    cancel.focus();
  });
  cancel.addEventListener("click", function () { dialog.close(); });
  dialog.addEventListener("click", function (ev) {
    if (ev.target !== dialog) {
      return;
    }
    // The dialog's own padding is the dialog too: close only for a click
    // outside its box, on the backdrop.
    var r = dialog.getBoundingClientRect();
    if (ev.clientX < r.left || ev.clientX > r.right || ev.clientY < r.top || ev.clientY > r.bottom) {
      dialog.close();
    }
  });
  dialog.addEventListener("close", function () {
    // The close event comes a task after close(): when a new question has
    // reopened the dialog since, this one is stale and must leave it be.
    if (dialog.open) {
      return;
    }
    var p = pending;
    pending = null;
    if (p && p.submitter && p.submitter.isConnected) {
      p.submitter.focus({ preventScroll: true });
    }
  });
  ok.addEventListener("click", function () {
    var p = pending;
    pending = null;
    dialog.close();
    if (!p) {
      return;
    }
    var form = p.form;
    var submitter = p.submitter;
    if (!form.isConnected) {
      // A live refresh swapped the form out: submit its replacement, found
      // by its action, with the same button.
      var action = form.getAttribute("action");
      var again = action ? document.querySelector('form[data-confirm][action="' + CSS.escape(action) + '"]') : null;
      if (!again) {
        if (window.vqToast) {
          window.vqToast("That item changed; try again.");
        }
        return;
      }
      form = again;
      submitter = null;
      if (p.submitter && p.submitter.name) {
        var buttons = form.querySelectorAll("button[name], input[name]");
        for (var i = 0; i < buttons.length; i++) {
          if (buttons[i].name === p.submitter.name && buttons[i].value === p.submitter.value) {
            submitter = buttons[i];
          }
        }
      } else if (p.submitter) {
        submitter = form.querySelector('button[type="submit"], button:not([type])');
      }
    }
    form.dataset.confirmed = "1";
    // requestSubmit fires submit at once, which clears the flag; when a
    // field fails validation no submit follows, so clear it anyway and the
    // next submit asks again.
    setTimeout(function () {
      delete form.dataset.confirmed;
    }, 0);
    if (submitter && submitter.form === form) {
      form.requestSubmit(submitter);
    } else {
      form.requestSubmit();
    }
  });
})();

// The ticket page's folded link form: opening it hides its button (CSS),
// so the cursor goes to the ticket box instead of being lost.
document.addEventListener("toggle", function (ev) {
  var d = ev.target;
  if (!d.matches || !d.matches("details[data-link-add]") || !d.open) {
    return;
  }
  var box = d.querySelector('input[name="to"]');
  if (box) {
    box.focus();
  }
}, true);

// Keyboard shortcuts (listed in the ? dialog): c new ticket, / search,
// g f Focus, g b board, g l list, g i inbox, j/k next/previous card (enter
// on its focused link opens it), ? shortcuts.
// Ignored while typing in a field, except Cmd+K or Ctrl+K, which focuses
// the search box from anywhere.
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
  function focusSearch() {
    var box = document.getElementById("vq-search");
    if (!box) {
      return;
    }
    if (!box.checkVisibility || !box.checkVisibility()) {
      document.dispatchEvent(new CustomEvent("vq:sidebar-open"));
    }
    box.focus();
  }
  document.addEventListener("keydown", function (ev) {
    if ((ev.metaKey || ev.ctrlKey) && !ev.altKey && !ev.shiftKey && (ev.key === "k" || ev.key === "K")) {
      if (!document.querySelector("dialog[open]")) {
        ev.preventDefault();
        focusSearch();
      }
      return;
    }
    if (ev.metaKey || ev.ctrlKey || ev.altKey || typing(ev.target)) {
      return;
    }
    if (document.querySelector("dialog[open]")) {
      return; // an open dialog closes itself on esc
    }
    var k = ev.key;
    var p = project();
    if (pendingG) {
      pendingG = false;
      if (k === "f") {
        window.location.href = p ? "/p/" + p : "/";
      } else if (k === "b") {
        window.location.href = p ? "/p/" + p + "/board" : "/";
      } else if (k === "l") {
        window.location.href = p ? "/p/" + p + "/list" : "/";
      } else if (k === "i") {
        window.location.href = "/inbox";
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
        focusSearch();
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
  document.body.addEventListener("htmx:beforeSwap", function (ev) {
    if (!ev.detail.shouldSwap) {
      return;
    }
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

// The sidebar (VAQ-171). From md it sits beside the page: the current
// project is scrolled into view inside it. Below md it is a drawer: the
// top bar's menu button opens it over a backdrop (data-open on the
// sidebar, aria-expanded on the button); a link in it, Escape and the
// backdrop close it. Without JavaScript the menu button is a link home.
// A project's Focus, board and list record themselves as its last view
// (vq:last-view:{KEY}); its other pages (a ticket, settings) mark that
// view in the sidebar, Focus until one is recorded (ruling T-R2).
(function () {
  var side = document.getElementById("vq-sidebar");
  var menu = document.querySelector("[data-menu-button]");
  var backdrop = document.querySelector("[data-sidebar-backdrop]");
  if (!side) {
    return;
  }
  var project = side.dataset.placeProject;
  var view = side.dataset.placeView;
  try {
    if (project && (view === "focus" || view === "board" || view === "list")) {
      localStorage.setItem("vq:last-view:" + project, view);
    } else if (project && side.querySelector("[data-last-view]")) {
      var last = localStorage.getItem("vq:last-view:" + project);
      var to = last && side.querySelector('[data-nav-view="' + last + '"]');
      if (to) {
        side.querySelectorAll("[data-nav-view][data-current]").forEach(function (a) {
          a.removeAttribute("data-current");
        });
        to.setAttribute("data-current", "");
      }
    }
  } catch (e) {
    // No storage (a private window): Focus stays marked.
  }
  function reveal() {
    var cur = side.querySelector('[data-nav-project][aria-current="page"]');
    if (!cur || !side.checkVisibility()) {
      return;
    }
    var s = side.getBoundingClientRect();
    var r = cur.getBoundingClientRect();
    if (r.top < s.top || r.bottom > s.bottom) {
      side.scrollTop += r.top - s.top - (s.height - r.height) / 2;
    }
  }
  reveal();
  function isOpen() {
    return side.hasAttribute("data-open");
  }
  function open() {
    side.setAttribute("data-open", "");
    if (backdrop) {
      backdrop.hidden = false;
    }
    if (menu) {
      menu.setAttribute("aria-expanded", "true");
    }
    reveal();
  }
  function close(refocus) {
    if (!isOpen()) {
      return;
    }
    side.removeAttribute("data-open");
    if (backdrop) {
      backdrop.hidden = true;
    }
    if (menu) {
      menu.setAttribute("aria-expanded", "false");
      if (refocus) {
        menu.focus();
      }
    }
  }
  document.addEventListener("vq:sidebar-open", open);
  if (menu) {
    menu.addEventListener("click", function (ev) {
      ev.preventDefault();
      if (isOpen()) {
        close(false);
      } else {
        open();
        var first = side.querySelector("a");
        if (first) {
          first.focus({ preventScroll: true });
        }
      }
    });
  }
  if (backdrop) {
    backdrop.addEventListener("click", function () { close(false); });
  }
  side.addEventListener("click", function (ev) {
    if (ev.target.closest && ev.target.closest("a")) {
      close(false);
    }
  });
  document.addEventListener("keydown", function (ev) {
    if (ev.key === "Escape" && isOpen() && !document.querySelector("dialog[open]")) {
      ev.preventDefault();
      close(true);
    }
  });
  // Grown past md, the drawer is the sidebar again.
  window.addEventListener("resize", function () {
    if (isOpen() && window.matchMedia("(min-width: 48rem)").matches) {
      close(false);
    }
  });
})();

// Filter menus (Priority, Milestone, Tags, Status): disclosures holding
// links. One is open at a time; a click outside it or esc closes it (esc
// puts focus back on its button). Opening the Tags menu focuses its search
// box, which hides the tags that don't contain what is typed; Enter there
// follows the first tag still shown.
(function () {
  function openMenus() {
    return document.querySelectorAll("details[data-menu][open]");
  }
  function visibleItems(menu) {
    return Array.prototype.filter.call(menu.querySelectorAll("[data-menu-item]"), function (li) {
      return !li.hidden;
    });
  }
  document.addEventListener("toggle", function (ev) {
    var d = ev.target;
    if (!d.matches || !d.matches("details[data-menu]") || !d.open) {
      return;
    }
    openMenus().forEach(function (o) {
      if (o !== d) {
        o.open = false;
      }
    });
    var box = d.querySelector("[data-menu-search]");
    if (box) {
      box.focus({ preventScroll: true });
    }
  }, true);
  document.addEventListener("click", function (ev) {
    openMenus().forEach(function (d) {
      if (!d.contains(ev.target)) {
        d.open = false;
      }
    });
  });
  document.addEventListener("keydown", function (ev) {
    if (ev.key !== "Escape" || ev.defaultPrevented) {
      return;
    }
    var open = openMenus();
    if (!open.length) {
      return;
    }
    ev.preventDefault();
    open.forEach(function (d) {
      d.open = false;
      var s = d.querySelector("summary");
      if (s) {
        s.focus({ preventScroll: true });
      }
    });
  });
  document.addEventListener("input", function (ev) {
    var box = ev.target;
    if (!box.matches || !box.matches("[data-menu-search]")) {
      return;
    }
    var menu = box.closest("details[data-menu]");
    var q = box.value.trim().toLowerCase();
    var shown = 0;
    menu.querySelectorAll("[data-menu-item]").forEach(function (li) {
      var hit = !q || li.getAttribute("data-menu-item").toLowerCase().indexOf(q) >= 0;
      li.hidden = !hit;
      if (hit) {
        shown += 1;
      }
    });
    var empty = menu.querySelector("[data-menu-empty]");
    if (empty) {
      empty.hidden = shown > 0;
    }
  });
  document.addEventListener("keydown", function (ev) {
    var box = ev.target;
    if (ev.key !== "Enter" || !box.matches || !box.matches("[data-menu-search]")) {
      return;
    }
    ev.preventDefault();
    var first = visibleItems(box.closest("details[data-menu]"))[0];
    var link = first && first.querySelector("a");
    if (link) {
      link.click();
    }
  });
})();

// Focus: a live refresh replaces #focus, and the disclosures that were
// open ("N more ready", "M waiting", a group's "N more") stay open.
(function () {
  var keep = null;
  document.body.addEventListener("htmx:beforeSwap", function (ev) {
    var target = ev.detail && ev.detail.target;
    if (!target || target.id !== "focus" || !ev.detail.shouldSwap) {
      return;
    }
    keep = Array.prototype.map.call(target.querySelectorAll("details[data-keep][open]"), function (d) {
      return d.dataset.keep;
    });
  });
  document.body.addEventListener("htmx:afterSwap", function () {
    var focus = document.getElementById("focus");
    if (!focus || keep === null) {
      return;
    }
    keep.forEach(function (name) {
      var d = focus.querySelector('details[data-keep="' + name + '"]');
      if (d) {
        d.open = true;
      }
    });
    keep = null;
  });
})();

// The list: each status group is a disclosure, and which are open is
// remembered per project in this browser (a click on a group's header
// records it; a group the list is filtered to stays open). Done and
// cancelled groups show 50 tickets and load 50 more at a time; a live
// refresh of #list says how many each shows (shown=STATUS:N), so the
// server renders as many again.
(function () {
  function list() {
    return document.getElementById("list");
  }
  function storageKey(el) {
    return "vq:list-groups:" + (el.dataset.listProject || "");
  }
  function load(el) {
    try {
      return JSON.parse(window.localStorage.getItem(storageKey(el)) || "{}") || {};
    } catch (e) {
      return {};
    }
  }
  function save(el, state) {
    try {
      window.localStorage.setItem(storageKey(el), JSON.stringify(state));
    } catch (e) {
      // Storage refused (a private window): the groups just aren't remembered.
    }
  }
  function apply() {
    var el = list();
    if (!el) {
      return;
    }
    var state = load(el);
    el.querySelectorAll("details[data-list-group]").forEach(function (d) {
      var id = d.dataset.listGroup;
      if (!d.hasAttribute("data-force-open") && Object.prototype.hasOwnProperty.call(state, id)) {
        d.open = !!state[id];
      }
    });
  }
  document.addEventListener("click", function (ev) {
    var s = ev.target.closest ? ev.target.closest("details[data-list-group] > summary") : null;
    var el = list();
    if (!s || !el) {
      return;
    }
    var d = s.parentElement;
    var state = load(el);
    state[d.dataset.listGroup] = !d.open; // the click is about to toggle it
    save(el, state);
  });
  document.body.addEventListener("htmx:configRequest", function (ev) {
    var elt = ev.detail && ev.detail.elt;
    if (!elt || elt.id !== "list") {
      return;
    }
    var shown = [];
    elt.querySelectorAll("details[data-list-group][data-paged]").forEach(function (d) {
      var n = d.querySelectorAll("[data-card]").length;
      if (n > 0) {
        shown.push(d.dataset.listGroup + ":" + n);
      }
    });
    if (shown.length) {
      ev.detail.parameters.shown = shown.join(",");
    }
  });
  // Applied straight after the swap, before the new groups' toggle events.
  document.body.addEventListener("htmx:afterSwap", apply);
  apply();
})();

// Rails below md: an open rail's cards overlay the columns (see the rail
// template), so one is open at a time, and its close control or Escape
// closes it and puts focus back on its strip. From md rails open in place
// and these do nothing.
(function () {
  var narrow = window.matchMedia("not all and (min-width: 48rem)");
  function closeRail(d) {
    d.open = false;
    var s = d.querySelector("summary");
    if (s) {
      s.focus({ preventScroll: true });
    }
  }
  // The rail opened most recently: toggle events come a task late, so two
  // quick taps would otherwise let the first rail's event close the second.
  var latest = null;
  function onlyOpen(d) {
    document.querySelectorAll("details[data-rail][open]").forEach(function (o) {
      if (o !== d) {
        o.open = false;
      }
    });
  }
  document.addEventListener("click", function (ev) {
    var btn = ev.target.closest ? ev.target.closest("[data-rail-close]") : null;
    var d = btn ? btn.closest("details[data-rail]") : null;
    if (d) {
      ev.preventDefault();
      closeRail(d);
      return;
    }
    // A tap on a closed rail's strip opens it once this click is done:
    // close the others now, in the same task.
    var s = ev.target.closest ? ev.target.closest("details[data-rail] > summary") : null;
    d = s ? s.parentElement : null;
    if (!narrow.matches || !d || d.open || ev.defaultPrevented) {
      return;
    }
    latest = d;
    onlyOpen(d);
  });
  // A rail opened another way (the keyboard opens one through a click
  // too). toggle doesn't bubble: listen in the capture phase.
  document.addEventListener("toggle", function (ev) {
    var d = ev.target;
    if (!narrow.matches || !d.matches || !d.matches("details[data-rail]") || !d.open) {
      return;
    }
    if (latest && latest !== d && latest.open && latest.isConnected) {
      d.open = false; // a later tap opened another rail
      return;
    }
    latest = d;
    onlyOpen(d);
  }, true);
  document.addEventListener("keydown", function (ev) {
    if (ev.key !== "Escape" || !narrow.matches || ev.defaultPrevented) {
      return;
    }
    if (document.querySelector("dialog[open]")) {
      return; // an open dialog closes itself
    }
    var open = document.querySelector("details[data-rail][open]");
    if (open) {
      ev.preventDefault();
      closeRail(open);
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
  var toast = window.vqToast;

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

// A refused form re-renders its page with 422 at the top, so its message
// can be off-screen (VAQ-209). On load the first invalid field in main is
// scrolled to the middle and focused (without a second scroll); with none,
// the first error alert in main (never the toast, which sits outside it)
// is scrolled into view. A page with neither never scrolls.
(function () {
  function show() {
    var main = document.querySelector("main");
    if (!main) {
      return;
    }
    var field = main.querySelector('[aria-invalid="true"]');
    if (field) {
      field.scrollIntoView({ block: "center" });
      field.focus({ preventScroll: true });
      return;
    }
    var alert = main.querySelector('[role="alert"]');
    if (alert) {
      alert.scrollIntoView({ block: "center" });
    }
  }
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", show);
  } else {
    show();
  }
})();
