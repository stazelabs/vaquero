package main

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/client"
)

const initLine = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`

// bridge runs vaquero mcp with in as standard input.
func bridge(t *testing.T, in io.Reader, args ...string) (int, string, string) {
	t.Helper()
	stdin = in
	defer func() { stdin = nil }()
	var o, e bytes.Buffer
	code := run(append([]string{"mcp"}, args...), &o, &e)
	return code, o.String(), e.String()
}

type rpcAnswer struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func answers(t *testing.T, out string) []rpcAnswer {
	t.Helper()
	var as []rpcAnswer
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		var a rpcAnswer
		if err := json.Unmarshal([]byte(line), &a); err != nil || a.JSONRPC != "2.0" {
			t.Fatalf("not a JSON-RPC line: %q (%v)", line, err)
		}
		as = append(as, a)
	}
	return as
}

func TestMCPBridgeRoundTrip(t *testing.T) {
	cliEnv(t)
	mustCLI(t, "ticket", "new", "VAQ", "Bridge me")
	in := strings.Join([]string{
		initLine,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"add_comment","arguments":{"key":"VAQ-1","body":"Via stdio."}}}`,
	}, "\n") + "\n"
	code, out, errOut := bridge(t, strings.NewReader(in), "--agent", "builder")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	as := answers(t, out)
	if len(as) != 3 || string(as[0].ID) != "1" || string(as[1].ID) != "2" || string(as[2].ID) != "3" {
		t.Fatalf("%s", out)
	}
	if !strings.Contains(string(as[1].Result), "claim_ticket") || as[2].Error != nil || strings.Contains(string(as[2].Result), `"isError":true`) {
		t.Fatalf("%s", out)
	}
	if show := mustCLI(t, "ticket", "show", "VAQ-1"); !strings.Contains(show, "builder") || !strings.Contains(show, "Via stdio.") {
		t.Fatalf("%s", show)
	}
}

// Review Focus 5: stdout carries only JSON-RPC, even for a line that isn't
// JSON or a request the server refuses.
func TestMCPBridgeStdoutClean(t *testing.T) {
	cliEnv(t)
	in := initLine + "\nnot json at all\n" + `[{"jsonrpc":"2.0","id":7,"method":"tools/list"}]` + "\n" + `{"jsonrpc":"2.0","id":8,"method":"tools/list"}` + "\n"
	code, out, _ := bridge(t, strings.NewReader(in))
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	as := answers(t, out)
	if len(as) != 4 || as[1].Error == nil || as[1].Error.Code != -32700 || as[2].Error == nil || string(as[3].ID) != "8" || as[3].Error != nil {
		t.Fatalf("%s", out)
	}
}

// Review Focus 5: closing stdin ends the bridge promptly.
func TestMCPBridgeEOF(t *testing.T) {
	cliEnv(t)
	r, w := io.Pipe()
	done := make(chan int, 1)
	go func() { code, _, _ := bridge(t, r); done <- code }()
	go w.Write([]byte(initLine + "\n"))
	time.Sleep(100 * time.Millisecond)
	w.Close()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit %d", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the bridge didn't end when stdin closed")
	}
}

func TestMCPBridgeServerDown(t *testing.T) {
	cliEnv(t)
	cfg := client.Config{Profiles: []client.Profile{{Name: "gone", URL: "http://127.0.0.1:1", Token: "vq_x"}}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := bridge(t, strings.NewReader(initLine+"\n"), "--profile", "gone")
	if code != 1 || out != "" || !strings.Contains(errOut, "127.0.0.1:1") {
		t.Fatalf("exit %d out %q err %q", code, out, errOut)
	}
}
