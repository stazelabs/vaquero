// Package client is the vaquero CLI's side of the API: an HTTP client that
// speaks /api/v1, the profiles saying which server to talk to, and
// starting the local server when it isn't running.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Client talks to one vaquero server.
type Client struct {
	BaseURL string // "http://127.0.0.1:7878"
	Token   string // bearer token
	Agent   string // an agent's name (local mode), sent as Vaquero-Agent
	Version string // this client's release, sent as Vaquero-Client
	HTTP    *http.Client

	minClient string // the last answer's Vaquero-Min-Client
}

// Error is an API error answer.
type Error struct {
	Status  int
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields"`
	Current json.RawMessage   `json:"current"`
}

// Error is the message, followed by any field messages it doesn't already
// say, in field order.
func (e *Error) Error() string {
	var parts []string
	for _, k := range slices.Sorted(maps.Keys(e.Fields)) {
		if v := e.Fields[k]; !strings.Contains(e.Message, v) {
			parts = append(parts, k+": "+v)
		}
	}
	if len(parts) == 0 {
		return e.Message
	}
	return e.Message + " (" + strings.Join(parts, "; ") + ")"
}

// Do sends body (nil, or encoded as JSON) to method path and decodes a 2xx
// answer into out (when out isn't nil). Any other answer is *Error.
func (c *Client) Do(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if c.Agent != "" {
		req.Header.Set("Vaquero-Agent", c.Agent)
	}
	if c.Version != "" {
		req.Header.Set("Vaquero-Client", c.Version)
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	res, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("can't reach %s: %w", c.BaseURL, err)
	}
	defer res.Body.Close()
	c.minClient = res.Header.Get("Vaquero-Min-Client")
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		if out == nil || len(bytes.TrimSpace(data)) == 0 {
			return nil
		}
		return json.Unmarshal(data, out)
	}
	var wrapped struct{ Error *Error }
	if json.Unmarshal(data, &wrapped) != nil || wrapped.Error == nil {
		return &Error{Status: res.StatusCode, Code: "http_error", Message: fmt.Sprintf("The server answered %d %s.", res.StatusCode, http.StatusText(res.StatusCode))}
	}
	wrapped.Error.Status = res.StatusCode
	return wrapped.Error
}

// UpgradeNeeded reports whether this client is older than the server's
// minimum, as of the last answer.
func (c *Client) UpgradeNeeded() (min string, need bool) {
	if c.minClient == "" || c.Version == "" {
		return c.minClient, false
	}
	return c.minClient, older(c.Version, c.minClient)
}

// older compares the major.minor.patch of two releases, ignoring any
// "-suffix".
func older(a, b string) bool {
	pa, pb := semver(a), semver(b)
	for i := range 3 {
		if pa[i] != pb[i] {
			return pa[i] < pb[i]
		}
	}
	return false
}

func semver(v string) [3]int {
	v, _, _ = strings.Cut(strings.TrimPrefix(v, "v"), "-")
	var out [3]int
	for i, part := range strings.SplitN(v, ".", 3) {
		out[i], _ = strconv.Atoi(part)
	}
	return out
}
