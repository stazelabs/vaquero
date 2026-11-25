package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net"
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

// Review fix: when the local server stops mid-session, the next message
// starts it again instead of failing until the client restarts the bridge.
func TestMCPBridgeRestartsLocalServer(t *testing.T) {
	dataDir, addr := localEnv(t)
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	stdin = inR
	defer func() { stdin = nil }()
	done := make(chan int, 1)
	var errOut bytes.Buffer
	go func() { done <- run([]string{"mcp"}, outW, &errOut); outW.Close() }()
	lines := bufio.NewReader(outR)
	send := func(s string) {
		go inW.Write([]byte(s + "\n"))
	}
	read := func() rpcAnswer {
		t.Helper()
		l, err := lines.ReadString('\n')
		if err != nil {
			t.Fatalf("%v; stderr %s", err, errOut.String())
		}
		return answers(t, l)[0]
	}
	send(initLine)
	if a := read(); a.Error != nil {
		t.Fatalf("%+v", a)
	}
	killTestServer(dataDir)
	for range 50 {
		c, err := net.Dial("tcp", addr)
		if err != nil {
			break
		}
		c.Close()
		time.Sleep(100 * time.Millisecond)
	}
	send(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"list_projects","arguments":{}}}`)
	if a := read(); a.Error != nil || strings.Contains(string(a.Result), `"isError":true`) {
		t.Fatalf("after the server stopped: %+v %s; stderr %s", a.Error, a.Result, errOut.String())
	}
	inW.Close()
	if code := <-done; code != 0 {
		t.Fatalf("exit %d", code)
	}
}

// VAQ-65: a line over the limit gets a JSON-RPC error (id null) and the
// bridge keeps reading; the line after it is answered.
func TestMCPBridgeOversizedLine(t *testing.T) {
	cliEnv(t)
	big := `{"jsonrpc":"2.0","id":9,"method":"x","params":"` + strings.Repeat("a", mcpLineMax+10) + `"}`
	in := initLine + "\n" + big + "\n" + `{"jsonrpc":"2.0","id":8,"method":"tools/list"}` + "\n"
	code, out, errOut := bridge(t, strings.NewReader(in))
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	as := answers(t, out)
	if len(as) != 3 || as[1].Error == nil || string(as[1].ID) != "null" || !strings.Contains(as[1].Error.Message, "too long") ||
		string(as[2].ID) != "8" || as[2].Error != nil {
		t.Fatalf("%.300s", out)
	}
}

// VAQ-65: a request still in flight when stdin closes is answered first.
func TestMCPBridgeAnswersBeforeExit(t *testing.T) {
	cliEnv(t)
	r, w := io.Pipe()
	go func() { _, _ = w.Write([]byte(initLine)); w.Close() }() // no trailing newline: EOF right after
	code, out, errOut := bridge(t, r)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if as := answers(t, out); len(as) != 1 || string(as[0].ID) != "1" {
		t.Fatalf("%s", out)
	}
}

// VAQ-67: --agent with a token that isn't the owner's is ignored, and the
// bridge says so on stderr, once; stdout stays JSON-RPC.
func TestMCPBridgeWarnsWhenAgentIgnored(t *testing.T) {
	e := cliEnv(t)
	bot, err := e.Deps.Core.EnsureAgent(t.Context(), e.Deps.Actor.ID, "ci-bot")
	if err != nil {
		t.Fatal(err)
	}
	raw, _, err := e.Deps.Core.CreateToken(t.Context(), e.Deps.Actor.ID, bot.ID, "bot")
	if err != nil {
		t.Fatal(err)
	}
	cfg := client.Config{Profiles: []client.Profile{{Name: "bot", URL: e.Server.URL, Token: raw}}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := bridge(t, strings.NewReader(initLine+"\n"), "--profile", "bot", "--agent", "builder")
	if code != 0 || strings.Count(errOut, "the agent name is ignored: this profile's token isn't the local owner's") != 1 || len(answers(t, out)) != 1 {
		t.Fatalf("exit %d\nerr %s\nout %s", code, errOut, out)
	}
	// With the owner's token the agent applies, and nothing is warned.
	if _, _, errOut := bridge(t, strings.NewReader(initLine+"\n"), "--profile", "t", "--agent", "builder"); strings.Contains(errOut, "ignored") {
		t.Fatalf("warned for the owner: %s", errOut)
	}
}
