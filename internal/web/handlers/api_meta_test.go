package handlers_test

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/api"
)

// TestEventsAPIIncludesMeta: each change records where it came from (the
// surface and the reported client, harness, model and run), and the events
// API shows it; an event with none has no meta key.
func TestEventsAPIIncludesMeta(t *testing.T) {
	e, c, tok := apiWorld(t)
	before := c.API("GET", "/api/v1/events?newest=1", tok, nil).Body
	if strings.Contains(before, `"meta"`) {
		t.Fatalf("an event made in core with no origin shows meta: %s", before)
	}

	req, err := http.NewRequest("POST", e.Server.URL+"/api/v1/tickets/VAQ-1/comments", bytes.NewReader([]byte(`{"body":"From a harness."}`)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Vaquero-Client", "vaquero-cli/0.1.6")
	req.Header.Set("Vaquero-Harness", "claude-code/2.1.3")
	req.Header.Set("Vaquero-Model", "claude-opus-5-5")
	req.Header.Set("Vaquero-Run", "run-l")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 201 && res.StatusCode != 200 {
		t.Fatalf("%d %s", res.StatusCode, b)
	}
	var evs []api.Event
	decode(t, c.API("GET", "/api/v1/events?newest=1", tok, nil).Body, &evs)
	want := map[string]string{"surface": "cli", "client": "vaquero-cli/0.1.6", "harness": "claude-code/2.1.3", "model": "claude-opus-5-5", "run": "run-l"}
	if len(evs) != 1 || len(evs[0].Meta) != len(want) {
		t.Fatalf("%+v", evs)
	}
	for k, v := range want {
		if evs[0].Meta[k] != v {
			t.Fatalf("meta %v, want %v", evs[0].Meta, want)
		}
	}

	// A web form post records surface web.
	if r := c.Post("/t/VAQ-1/comments", url.Values{"body": {"From the page."}}); r.Status != 303 {
		t.Fatalf("comment: %d", r.Status)
	}
	evs = nil
	decode(t, c.API("GET", "/api/v1/events?newest=1", tok, nil).Body, &evs)
	if len(evs) != 1 || len(evs[0].Meta) != 1 || evs[0].Meta["surface"] != "web" {
		t.Fatalf("%+v", evs)
	}
}
