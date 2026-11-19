package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/stazelabs/vaquero/internal/version"
)

func init() { cliCommands["mcp"] = cmdMCP }

// mcpLineMax is the longest JSON-RPC message the bridge reads.
const mcpLineMax = 4 << 20

// cmdMCP is vaquero mcp: a stdio MCP server for clients that launch one,
// bridging to the server's /mcp. It connects like any command (starting
// the local server when needed) and checks the token, then reads one
// JSON-RPC message per line from standard input, posts it to /mcp with the
// token and agent name, and writes each answer as one line on standard
// output. Standard output carries only JSON-RPC; every diagnostic goes to
// standard error. A line that isn't JSON, or a request the server refuses,
// gets a JSON-RPC error for its id; a notification gets no answer. The end
// of standard input ends it with exit 0.
func cmdMCP(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	c := newCmd(stdout, stderr, "vaquero mcp [--profile NAME] [--agent NAME]")
	if _, err := c.parse(args, 0, 0); err != nil {
		return c.finish(err)
	}
	// Anything a command would print goes to stderr: stdout is the protocol's.
	c.stdout = stderr
	if err := c.connect(ctx); err != nil {
		return c.finish(err)
	}
	var me struct{ Actor struct{ Name string } }
	if err := c.call(ctx, "GET", "/api/v1/me", nil, &me); err != nil {
		return c.finish(err)
	}
	fmt.Fprintf(stderr, "vaquero mcp: connected to %s as %s\n", c.client.BaseURL, me.Actor.Name)

	in := stdin
	if in == nil {
		in = os.Stdin
	}
	b := &mcpBridge{
		url: strings.TrimRight(c.client.BaseURL, "/") + "/mcp", token: c.client.Token, agent: c.client.Agent,
		http: &http.Client{Timeout: 2 * time.Minute}, out: stdout, log: stderr, restart: c.restartLocal,
	}
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 64<<10), mcpLineMax)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		b.forward(ctx, line)
	}
	if err := sc.Err(); err != nil {
		fmt.Fprintf(stderr, "vaquero mcp: reading standard input: %v\n", err)
		return exitFail
	}
	return exitOK
}

// mcpBridge posts messages to /mcp and writes the answers.
type mcpBridge struct {
	url, token, agent string
	http              *http.Client
	out, log          io.Writer
	// restart, for the local profile, starts the server again when it
	// stopped mid-session, and returns the token to use.
	restart func(ctx context.Context) (string, error)
	// mute drops answers while a notification is forwarded.
	mute bool
}

// write sends one answer line, unless muted.
func (b *mcpBridge) write(line []byte) {
	if !b.mute {
		_, _ = b.out.Write(line)
	}
}

// forward sends one message and writes its answer, if it has one.
func (b *mcpBridge) forward(ctx context.Context, msg []byte) {
	var head struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	if msg[0] == '{' && json.Unmarshal(msg, &head) != nil || msg[0] != '{' && msg[0] != '[' || !json.Valid(msg) {
		b.fail(nil, -32700, "Parse error: send one JSON-RPC message per line.")
		return
	}
	// A notification (a message with no id) never gets an answer.
	if msg[0] == '{' && len(head.ID) == 0 {
		b.mute = true
		defer func() { b.mute = false }()
	}
	res, err := b.post(ctx, msg)
	if err != nil && b.restart != nil {
		// The local server may have stopped: start it once and retry.
		fmt.Fprintf(b.log, "vaquero mcp: %v; starting the local server\n", err)
		if tok, rerr := b.restart(ctx); rerr == nil {
			b.token = tok
			res, err = b.post(ctx, msg)
		} else {
			err = rerr
		}
	}
	if err != nil {
		fmt.Fprintf(b.log, "vaquero mcp: %v\n", err)
		b.fail(head.ID, -32603, "Vaquero couldn't be reached.")
		return
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, mcpLineMax))
	if err != nil {
		b.fail(head.ID, -32603, "The answer couldn't be read.")
		return
	}
	body = bytes.TrimSpace(body)
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		if len(body) == 0 {
			return // a notification's 202
		}
		var line bytes.Buffer
		if json.Compact(&line, body) != nil {
			b.fail(head.ID, -32603, "The server's answer wasn't JSON.")
			return
		}
		line.WriteByte('\n')
		b.write(line.Bytes())
		return
	}
	// A refusal: the API's envelope, or the SDK's plain text.
	msgText := strings.TrimSpace(string(body))
	var env struct{ Error struct{ Message string } }
	if json.Unmarshal(body, &env) == nil && env.Error.Message != "" {
		msgText = env.Error.Message
	}
	if msgText == "" {
		msgText = fmt.Sprintf("The server answered %d %s.", res.StatusCode, http.StatusText(res.StatusCode))
	}
	fmt.Fprintf(b.log, "vaquero mcp: %s\n", msgText)
	b.fail(head.ID, -32600, msgText)
}

// post sends msg to /mcp.
func (b *mcpBridge) post(ctx context.Context, msg []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.url, bytes.NewReader(msg))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+b.token)
	req.Header.Set("Vaquero-Client", version.Version)
	if b.agent != "" {
		req.Header.Set("Vaquero-Agent", b.agent)
	}
	return b.http.Do(req)
}

// fail writes a JSON-RPC error for id (null when it is unknown, as for a
// line that isn't JSON or a batch).
func (b *mcpBridge) fail(id json.RawMessage, code int, message string) {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	line, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": id,
		"error": map[string]any{"code": code, "message": message},
	})
	b.write(append(line, '\n'))
}
