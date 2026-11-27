package client

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOlderAndIsDev(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"0.1.0", "0.2.0", true}, {"0.2.0", "0.1.0", false}, {"0.2.0", "0.2.0", false},
		{"1.9.9", "1.10.0", true}, {"v0.1.0", "0.1.1", true},
	} {
		if got := Older(c.a, c.b); got != c.want {
			t.Errorf("Older(%q, %q) = %v", c.a, c.b, got)
		}
	}
	for v, want := range map[string]bool{"": true, "0.0.0-dev": true, "0.3.0-rc.1": true, "0.3.0": false} {
		if IsDev(v) != want {
			t.Errorf("IsDev(%q) = %v", v, !want)
		}
	}
}

func TestServerVersionFromLastAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Vaquero-Version", "0.4.2")
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL}
	if err := c.Do(t.Context(), "GET", "/api/v1/me", nil, nil); err != nil {
		t.Fatal(err)
	}
	if c.ServerVersion() != "0.4.2" {
		t.Fatalf("%q", c.ServerVersion())
	}
}

func TestDevBuildNeverNeedsUpgrade(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Vaquero-Min-Client", "99.0.0")
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, Version: "0.0.0-dev"}
	_ = c.Do(t.Context(), "GET", "/api/v1/me", nil, nil)
	if _, need := c.UpgradeNeeded(); need {
		t.Fatal("a dev build was told to upgrade")
	}
	c.Version = "1.0.0"
	_ = c.Do(t.Context(), "GET", "/api/v1/me", nil, nil)
	if _, need := c.UpgradeNeeded(); !need {
		t.Fatal("a release older than the minimum wasn't told to upgrade")
	}
}

// A server that stops while starting is reported at once, not after the
// whole wait: the wait is long so a first boot can migrate a big database.
func TestEnsureLocalReportsAStoppedServerAtOnce(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	start := time.Now()
	_, err := EnsureLocal(t.Context(), "/usr/bin/false", "http://"+addr, t.TempDir(), 30*time.Second)
	if err == nil || !strings.Contains(err.Error(), "stopped while starting") {
		t.Fatalf("%v", err)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("took %v to notice", took)
	}
}
