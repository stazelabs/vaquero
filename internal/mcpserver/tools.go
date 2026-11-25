package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/web/middleware"
)

// The tools' arguments. A field without omitempty is required; the
// jsonschema tag is the argument's description.
type (
	noArgs struct{}

	keyArgs struct {
		Key string `json:"key" jsonschema:"The ticket's key, such as VAQ-12."`
	}

	listTicketsArgs struct {
		Project string `json:"project" jsonschema:"The project's key, such as VAQ."`
		Status  string `json:"status,omitempty" jsonschema:"Only tickets in this status, by name, such as In progress."`
		Tag     string `json:"tag,omitempty" jsonschema:"Only tickets with this tag."`
		Mine    bool   `json:"mine,omitempty" jsonschema:"Only tickets assigned to you."`
		Open    bool   `json:"open,omitempty" jsonschema:"Leave out done and cancelled tickets."`
		Limit   int64  `json:"limit,omitempty" jsonschema:"At most this many, up to 500."`
	}

	readyArgs struct {
		Project string `json:"project,omitempty" jsonschema:"Only this project's ready tickets, by key. Leave it out for every project."`
		Limit   int64  `json:"limit,omitempty" jsonschema:"At most this many, up to 500. Leave it out for 50."`
	}

	searchArgs struct {
		Query string `json:"query" jsonschema:"The words to find in ticket titles, descriptions and comments."`
	}

	activityArgs struct {
		After  int64 `json:"after,omitempty" jsonschema:"Only changes after this sequence number: pass the last seq you saw."`
		Newest int64 `json:"newest,omitempty" jsonschema:"The newest this many changes, oldest of them first."`
	}

	createArgs struct {
		Project   string   `json:"project" jsonschema:"The project's key, such as VAQ."`
		Title     string   `json:"title" jsonschema:"The ticket's title."`
		Body      string   `json:"body,omitempty" jsonschema:"The description, in Markdown."`
		Status    string   `json:"status,omitempty" jsonschema:"A status name; the project's first todo status if left out."`
		Priority  int64    `json:"priority,omitempty" jsonschema:"0 none, 1 urgent, 2 high, 3 medium, 4 low."`
		Assignee  string   `json:"assignee,omitempty" jsonschema:"Who to assign it to, by name."`
		Milestone string   `json:"milestone,omitempty" jsonschema:"A milestone, by name."`
		Parent    string   `json:"parent,omitempty" jsonschema:"The parent ticket's key, making this a sub-ticket."`
		Tags      []string `json:"tags,omitempty" jsonschema:"Tags; new ones are created."`
	}

	updateArgs struct {
		Key        string   `json:"key" jsonschema:"The ticket's key, such as VAQ-12."`
		Title      *string  `json:"title,omitempty" jsonschema:"A new title."`
		Body       *string  `json:"body,omitempty" jsonschema:"A new description, in Markdown."`
		Status     *string  `json:"status,omitempty" jsonschema:"A status name."`
		Priority   *int64   `json:"priority,omitempty" jsonschema:"0 none, 1 urgent, 2 high, 3 medium, 4 low."`
		Assignee   *string  `json:"assignee,omitempty" jsonschema:"Who to assign it to, by name; an empty string clears it."`
		Milestone  *string  `json:"milestone,omitempty" jsonschema:"A milestone, by name; an empty string clears it."`
		Parent     *string  `json:"parent,omitempty" jsonschema:"The parent ticket's key; an empty string clears it."`
		Tags       []string `json:"tags,omitempty" jsonschema:"Replace every tag with these."`
		AddTags    []string `json:"add_tags,omitempty" jsonschema:"Tags to add."`
		RemoveTags []string `json:"remove_tags,omitempty" jsonschema:"Tags to remove."`
		Version    int64    `json:"version,omitempty" jsonschema:"The version you read; the update is refused if the ticket changed since."`
		View       string   `json:"view,omitempty" jsonschema:"summary for a short answer: key, status, category, version, assignee and claim. Leave it out for the whole ticket."`
	}

	moveArgs struct {
		Key    string `json:"key" jsonschema:"The ticket's key, such as VAQ-12."`
		Status string `json:"status" jsonschema:"The status to move it to, by name."`
		View   string `json:"view,omitempty" jsonschema:"summary for a short answer: key, status, category, version, assignee and claim. Leave it out for the whole ticket."`
	}

	commentArgs struct {
		Key  string `json:"key" jsonschema:"The ticket's key, such as VAQ-12."`
		Body string `json:"body" jsonschema:"The comment, in Markdown."`
		View string `json:"view,omitempty" jsonschema:"summary for a short answer: the comment's id and the ticket's key, status, category, version, assignee and claim. Leave it out for the comment."`
	}

	linkArgs struct {
		Key  string `json:"key" jsonschema:"The ticket's key, such as VAQ-12."`
		Kind string `json:"kind" jsonschema:"blocks, relates or duplicates: this ticket blocks, relates to or duplicates the other."`
		To   string `json:"to" jsonschema:"The other ticket's key."`
	}

	leaseArgs struct {
		Key          string `json:"key" jsonschema:"The ticket's key, such as VAQ-12."`
		LeaseMinutes int64  `json:"lease_minutes,omitempty" jsonschema:"How long to hold it, 1 to 120 minutes. Leave it out for 15."`
		View         string `json:"view,omitempty" jsonschema:"summary for a short answer: key, status, category, version, assignee and claim. Leave it out for the whole ticket."`
	}

	releaseArgs struct {
		Key  string `json:"key" jsonschema:"The ticket's key, such as VAQ-12."`
		View string `json:"view,omitempty" jsonschema:"summary for a short answer: key, status, category, version, assignee and claim. Leave it out for the whole ticket."`
	}
)

// toolNames is every tool, sorted.
var toolNames = []string{
	"add_comment", "claim_ticket", "create_ticket", "get_ticket", "heartbeat", "link_tickets", "list_projects",
	"list_tickets", "move_ticket", "ready_tickets", "recent_activity", "release_ticket", "search", "update_ticket",
}

// ToolNames is every tool's name, sorted.
func ToolNames() []string { return append([]string(nil), toolNames...) }

// request is one /api/v1 call a tool makes.
type request struct {
	method, path string
	query        url.Values
	body         any
}

func get(path string, q url.Values) request {
	return request{method: http.MethodGet, path: path, query: q}
}
func post(path string, body any) request {
	return request{method: http.MethodPost, path: path, body: body}
}

// viewed is rq asking for view (?view=), when one is given.
func viewed(rq request, view string) request {
	if view != "" {
		rq.query = url.Values{"view": {view}}
	}
	return rq
}

// ticketPath is a ticket key's API path.
func ticketPath(key, rest string) string { return "/tickets/" + url.PathEscape(key) + rest }

// addTools adds every tool to s.
func addTools(s *mcp.Server, c *caller) {
	add(s, c, "list_projects", "List projects", true,
		"Every project, with its key, name and open ticket count.",
		func(noArgs) request { return get("/projects", nil) })
	add(s, c, "list_tickets", "List a project's tickets", true,
		"A project's tickets in board order, each with its key, title, status, priority, assignee, tags, whether it is ready, its claim and its version.",
		func(in listTicketsArgs) request {
			q := url.Values{}
			set(q, "status", in.Status)
			set(q, "tag", in.Tag)
			flag(q, "mine", in.Mine)
			flag(q, "open", in.Open)
			num(q, "limit", in.Limit)
			return get("/projects/"+url.PathEscape(in.Project)+"/tickets", q)
		})
	add(s, c, "get_ticket", "Get a ticket", true,
		"One ticket with its description, tags, sub-tickets, links, comments, attachments (name, type, size and url), claim and version.",
		func(in keyArgs) request { return get(ticketPath(in.Key, ""), nil) })
	add(s, c, "ready_tickets", "List ready tickets", true,
		"Tickets ready to be worked on: in a todo status, unassigned, unclaimed, not blocked and with no open sub-tickets. Most urgent first.",
		func(in readyArgs) request {
			q := url.Values{}
			set(q, "project", in.Project)
			num(q, "limit", in.Limit)
			return get("/ready", q)
		})
	add(s, c, "search", "Search", true,
		"Find tickets by words in their titles, descriptions and comments.",
		func(in searchArgs) request { return get("/search", url.Values{"q": {in.Query}}) })
	add(s, c, "recent_activity", "Recent activity", true,
		"Changes across every project, oldest first, each with a seq: pass after to see only newer ones, or newest for the latest few.",
		func(in activityArgs) request {
			q := url.Values{}
			num(q, "after", in.After)
			num(q, "newest", in.Newest)
			return get("/events", q)
		})
	add(s, c, "create_ticket", "Create a ticket", false,
		"Create a ticket in a project. Only the title is required.",
		func(in createArgs) request {
			b := map[string]any{"title": in.Title}
			for k, v := range map[string]string{"body": in.Body, "status": in.Status, "assignee": in.Assignee, "milestone": in.Milestone, "parent": in.Parent} {
				if v != "" {
					b[k] = v
				}
			}
			if in.Priority != 0 {
				b["priority"] = in.Priority
			}
			if in.Tags != nil {
				b["tags"] = in.Tags
			}
			return post("/projects/"+url.PathEscape(in.Project)+"/tickets", b)
		})
	add(s, c, "update_ticket", "Update a ticket", false,
		"Change only the fields given. Pass the version you read to have the change refused if someone else changed the ticket since.",
		func(in updateArgs) request {
			b := map[string]any{}
			for k, v := range map[string]*string{"title": in.Title, "body": in.Body, "status": in.Status, "assignee": in.Assignee, "milestone": in.Milestone, "parent": in.Parent} {
				if v != nil {
					b[k] = *v
				}
			}
			if in.Priority != nil {
				b["priority"] = *in.Priority
			}
			for k, v := range map[string][]string{"tags": in.Tags, "add_tags": in.AddTags, "remove_tags": in.RemoveTags} {
				if v != nil {
					b[k] = v
				}
			}
			if in.Version != 0 {
				b["version"] = in.Version
			}
			return viewed(request{method: http.MethodPatch, path: ticketPath(in.Key, ""), body: b}, in.View)
		})
	add(s, c, "move_ticket", "Move a ticket", false,
		"Move a ticket to another status, at the end of its column.",
		func(in moveArgs) request {
			return viewed(post(ticketPath(in.Key, "/move"), map[string]string{"status": in.Status}), in.View)
		})
	add(s, c, "add_comment", "Add a comment", false,
		"Add a comment to a ticket, in Markdown.",
		func(in commentArgs) request {
			return viewed(post(ticketPath(in.Key, "/comments"), map[string]string{"body": in.Body}), in.View)
		})
	add(s, c, "link_tickets", "Link two tickets", false,
		"Record that one ticket blocks, relates to or duplicates another. A blocking loop is refused.",
		func(in linkArgs) request {
			return post(ticketPath(in.Key, "/links"), map[string]string{"kind": in.Kind, "to": in.To})
		})
	add(s, c, "claim_ticket", "Claim a ticket", false,
		"Claim a ticket to work on it: it is assigned to you and moved to in progress, and no one else can claim it until your lease ends. Claiming your own claim renews it.",
		func(in leaseArgs) request {
			return viewed(post(ticketPath(in.Key, "/claim"), leaseBody(in.LeaseMinutes)), in.View)
		})
	add(s, c, "heartbeat", "Renew a claim", false,
		"Extend your claim on a ticket from now, so it doesn't lapse while you work.",
		func(in leaseArgs) request {
			return viewed(post(ticketPath(in.Key, "/heartbeat"), leaseBody(in.LeaseMinutes)), in.View)
		})
	add(s, c, "release_ticket", "Release a claim", false,
		"Give up your claim on a ticket, leaving its status and assignee as they are.",
		func(in releaseArgs) request { return viewed(post(ticketPath(in.Key, "/release"), nil), in.View) })
}

func set(q url.Values, k, v string) {
	if v != "" {
		q.Set(k, v)
	}
}

func flag(q url.Values, k string, v bool) {
	if v {
		q.Set(k, "1")
	}
}

func num(q url.Values, k string, v int64) {
	if v != 0 {
		q.Set(k, strconv.FormatInt(v, 10))
	}
}

func leaseBody(minutes int64) any {
	if minutes == 0 {
		return nil
	}
	return map[string]int64{"lease_minutes": minutes}
}

// annotations describe a tool: a read-only one changes nothing; none reach
// outside this Vaquero.
func annotations(title string, readOnly bool) *mcp.ToolAnnotations {
	closed := false
	a := &mcp.ToolAnnotations{Title: title, ReadOnlyHint: readOnly, OpenWorldHint: &closed}
	if readOnly {
		a.IdempotentHint = true
	} else {
		notDestructive := false
		a.DestructiveHint = &notDestructive
	}
	return a
}

// add adds a tool that makes the request build gives, as the caller.
func add[In any](s *mcp.Server, c *caller, name, title string, readOnly bool, desc string, build func(In) request) {
	mcp.AddTool(s, &mcp.Tool{Name: name, Title: title, Description: desc, Annotations: annotations(title, readOnly)},
		func(ctx context.Context, _ *mcp.CallToolRequest, in In) (res *mcp.CallToolResult, out any, err error) {
			defer recoverTool(ctx, c.logger, name, &res, &err)
			body, f := c.do(ctx, build(in))
			if f != nil {
				noteCode(ctx, f.Code)
				return nil, nil, f
			}
			if !json.Valid(body) {
				c.logger.ErrorContext(ctx, "mcp tool answer not json", "tool", name)
				return nil, nil, errSomething
			}
			// The structured content is set here, not returned as out: the
			// SDK would add a second text copy of a list.
			return &mcp.CallToolResult{
				Content:           []mcp.Content{&mcp.TextContent{Text: string(body)}},
				StructuredContent: structured(body),
			}, nil, nil
		})
}

// structured is a tool's REST answer as MCP structured content, which must
// be a JSON object: a list answer is wrapped as {"items": [...]}. The text
// content stays the REST answer exactly.
func structured(body []byte) json.RawMessage {
	b := bytes.TrimSpace(body)
	if len(b) > 0 && b[0] == '[' {
		return json.RawMessage(append(append([]byte(`{"items":`), b...), '}'))
	}
	return json.RawMessage(b)
}

// toolError is a refusal from the API, as the tool's error: its text is
// the API's message.
type toolError struct{ Code, Message string }

func (e *toolError) Error() string { return e.Message }

// errSomething is the answer to a failure whose cause stays in the log.
var errSomething = &toolError{Code: "server_error", Message: "Something went wrong."}

// caller makes a tool's API request in-process, as actor.
type caller struct {
	api    http.Handler
	actor  gen.Actor
	logger *slog.Logger
}

// do serves rq through the API and returns the answer's body, or the
// refusal.
func (c *caller) do(ctx context.Context, rq request) ([]byte, *toolError) {
	var body bytes.Buffer
	if rq.body != nil {
		if err := json.NewEncoder(&body).Encode(rq.body); err != nil {
			return nil, errSomething
		}
	}
	target := "/api/v1" + rq.path
	if len(rq.query) > 0 {
		target += "?" + rq.query.Encode()
	}
	// A fresh routing context: the /mcp request's would make the API's
	// router treat this call as a sub-route of /mcp.
	ctx = context.WithValue(middleware.WithAPIActor(ctx, c.actor), chi.RouteCtxKey, nil)
	req, err := http.NewRequestWithContext(ctx, rq.method, target, &body)
	if err != nil {
		return nil, &toolError{Code: "bad_request", Message: fmt.Sprintf("%s isn't a valid request.", rq.path)}
	}
	if rq.body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := &recorder{header: http.Header{}, status: http.StatusOK}
	c.api.ServeHTTP(rec, req)
	if rec.status >= 200 && rec.status < 300 {
		return rec.body.Bytes(), nil
	}
	var fail struct {
		Error struct {
			toolError
			Fields map[string]string `json:"fields"`
		} `json:"error"`
	}
	if json.Unmarshal(rec.body.Bytes(), &fail) != nil || fail.Error.Message == "" {
		c.logger.ErrorContext(ctx, "mcp api answer", "status", rec.status, "path", rq.path)
		return nil, errSomething
	}
	return nil, withFields(fail.Error.toolError, fail.Error.Fields)
}

// withFields is e with the API's field messages added to its text, one per
// line, when the message doesn't already say them (a refusal over several
// names says only that some don't match).
func withFields(e toolError, fields map[string]string) *toolError {
	for _, k := range slices.Sorted(maps.Keys(fields)) {
		if m := fields[k]; m != "" && !strings.Contains(e.Message, m) {
			e.Message += "\n" + m
		}
	}
	return &e
}

// recorder keeps an in-process answer.
type recorder struct {
	header http.Header
	status int
	body   bytes.Buffer
	wrote  bool
}

func (r *recorder) Header() http.Header { return r.header }
func (r *recorder) Write(b []byte) (int, error) {
	r.wrote = true
	return r.body.Write(b)
}
func (r *recorder) WriteHeader(code int) {
	if !r.wrote {
		r.status, r.wrote = code, true
	}
}

// recoverTool turns a panic in a tool into the plain tool error, logged.
func recoverTool(ctx context.Context, logger *slog.Logger, name string, res **mcp.CallToolResult, err *error) {
	v := recover()
	if v == nil {
		return
	}
	logger.ErrorContext(ctx, "mcp tool panic", "tool", name, "err", v, "stack", string(debug.Stack()))
	noteCode(ctx, errSomething.Code)
	*res, *err = nil, errSomething
}
