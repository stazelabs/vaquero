// Package e2e drives the real pages in a headless Chrome, with real key
// presses and mouse events. The tests are behind the e2e build tag
// (make e2e), boot webtest's server (a fixed clock, core to arrange
// state), and skip when no Chrome or Chromium is installed, unless
// VQ_E2E_REQUIRE_CHROME is set (the gate workflow sets it). They wait on
// the page's own state, never on a timer.
package e2e
