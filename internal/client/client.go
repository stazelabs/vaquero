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
	"mime/multipart"
	"net/http"
	"net/textproto"
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

	minClient     string   // the last answer's Vaquero-Min-Client
	serverVersion string   // the last answer's Vaquero-Version
	warnings      []string // warnings from answers since the last Warnings
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
	contentType := ""
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd, contentType = bytes.NewReader(b), "application/json"
	}
	return c.send(ctx, method, path, rd, contentType, out)
}

// Upload posts the bytes of r to path as a multipart form, in the part
// "file" named filename, and decodes a 2xx answer into out. The file is
// buffered, so a refusal for its size comes back as the server's *Error.
func (c *Client) Upload(ctx context.Context, path, filename string, r io.Reader, out any) error {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="file"; filename="`+strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\r", "", "\n", "").Replace(filename)+`"`)
	h.Set("Content-Type", "application/octet-stream")
	pw, err := mw.CreatePart(h)
	if err != nil {
		return err
	}
	if _, err := io.Copy(pw, r); err != nil {
		return err
	}
	if err := mw.Close(); err != nil {
		return err
	}
	return c.send(ctx, http.MethodPost, path, &buf, mw.FormDataContentType(), out)
}

// Download GETs path and copies a 2xx answer's body to w, with no overall
// time limit (ctx bounds it). Any other answer is *Error. A body that ends
// early (the server aborted it) is an error, so a caller writing a file
// must treat any error as a broken download.
func (c *Client) Download(ctx context.Context, path string, w io.Writer) (int64, error) {
	req, err := c.request(ctx, http.MethodGet, path, nil, "")
	if err != nil {
		return 0, err
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{}
	}
	res, err := hc.Do(req)
	if err != nil {
		return 0, fmt.Errorf("can't reach %s: %w", c.BaseURL, err)
	}
	defer res.Body.Close()
	c.minClient = res.Header.Get("Vaquero-Min-Client")
	c.serverVersion = res.Header.Get("Vaquero-Version")
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		data, err := io.ReadAll(res.Body)
		if err != nil {
			return 0, err
		}
		return 0, failure(res.StatusCode, data)
	}
	return io.Copy(w, res.Body)
}

// request builds one request with the client's headers.
func (c *Client) request(ctx context.Context, method, path string, body io.Reader, contentType string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
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
	return req, nil
}

// failure is the *Error for a non-2xx answer with body data.
func failure(status int, data []byte) *Error {
	var wrapped struct{ Error *Error }
	if json.Unmarshal(data, &wrapped) != nil || wrapped.Error == nil {
		return &Error{Status: status, Code: "http_error", Message: fmt.Sprintf("The server answered %d %s.", status, http.StatusText(status))}
	}
	wrapped.Error.Status = status
	return wrapped.Error
}

// send makes one request with body (nil for none) of contentType, and
// reads the answer as Do says.
func (c *Client) send(ctx context.Context, method, path string, body io.Reader, contentType string, out any) error {
	req, err := c.request(ctx, method, path, body, contentType)
	if err != nil {
		return err
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
	c.serverVersion = res.Header.Get("Vaquero-Version")
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		if len(bytes.TrimSpace(data)) == 0 {
			return nil
		}
		c.noteWarnings(data)
		if out == nil {
			return nil
		}
		return json.Unmarshal(data, out)
	}
	return failure(res.StatusCode, data)
}

// noteWarnings keeps an answer's "warning" (someone else holds a claim on
// the ticket; the write went through), and each bulk item's, named by key.
func (c *Client) noteWarnings(data []byte) {
	var a struct {
		Warning string
		Items   []struct{ Key, Warning string }
	}
	if json.Unmarshal(data, &a) != nil {
		return // an array, or not JSON: no warning to find
	}
	if a.Warning != "" {
		c.warnings = append(c.warnings, a.Warning)
	}
	for _, it := range a.Items {
		if it.Warning != "" {
			c.warnings = append(c.warnings, it.Key+": "+it.Warning)
		}
	}
}

// Warnings returns the warnings answers carried since the last call, and
// forgets them.
func (c *Client) Warnings() []string {
	w := c.warnings
	c.warnings = nil
	return w
}

// UpgradeNeeded reports whether this client is older than the server's
// minimum, as of the last answer. A dev build is never told to upgrade.
func (c *Client) UpgradeNeeded() (min string, need bool) {
	if c.minClient == "" || IsDev(c.Version) {
		return c.minClient, false
	}
	return c.minClient, Older(c.Version, c.minClient)
}

// ServerVersion is the server's release, as of the last answer.
func (c *Client) ServerVersion() string { return c.serverVersion }

// IsDev reports whether v is a development build rather than a release:
// empty, or with a pre-release part ("0.0.0-dev", "0.3.0-rc.1").
func IsDev(v string) bool { return v == "" || strings.Contains(v, "-") }

// Older compares the major.minor.patch of two releases, ignoring any
// "-suffix".
func Older(a, b string) bool {
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
