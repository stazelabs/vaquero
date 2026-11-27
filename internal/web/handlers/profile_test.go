package handlers_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/web/webtest"
)

func TestProfilePage(t *testing.T) {
	e := webtest.New(t)
	c := e.Client()
	page := c.Get("/profile")
	if page.Status != 200 {
		t.Fatalf("%d", page.Status)
	}
	for _, want := range []string{`action="/profile"`, `name="display_name"`, `action="/profile/avatar"`, `enctype="multipart/form-data"`, "Save profile", "Upload avatar", `data-avatar-monogram="OW"`} {
		if !strings.Contains(page.Body, want) {
			t.Errorf("profile page lacks %s", want)
		}
	}
	// The header links to the profile from every page.
	if home := c.Get("/").Body; !strings.Contains(home, `href="/profile"`) {
		t.Error("no header link to /profile")
	}
	r := c.Post("/profile", url.Values{"display_name": {"Kim Lee"}})
	if r.Status != http.StatusSeeOther || r.Location != "/profile" {
		t.Fatalf("%d %q", r.Status, r.Location)
	}
	if body := c.Follow(r).Body; !strings.Contains(body, "Profile saved.") || !strings.Contains(body, `value="Kim Lee"`) || !strings.Contains(body, `data-avatar-monogram="KL"`) {
		t.Fatal("name not shown after save")
	}
	if a, _ := e.Deps.R.GetActor(bg, e.Deps.Actor.ID); a.DisplayName != "Kim Lee" || a.Name != "Owner" {
		t.Fatalf("%+v", a)
	}
	r = c.Post("/profile", url.Values{"display_name": {strings.Repeat("k", 81)}})
	if r.Status != http.StatusUnprocessableEntity || !strings.Contains(r.Body, "Keep the name to 80 characters.") {
		t.Fatalf("%d", r.Status)
	}
	if r := c.PostCrossSite("/profile", url.Values{"display_name": {"Mallory"}}); r.Status != http.StatusForbidden {
		t.Fatalf("cross-site: %d", r.Status)
	}
}

func TestProfileAvatarWeb(t *testing.T) {
	e := webtest.New(t)
	c := e.Client()
	avatar := "/avatars/" + e.Deps.Actor.ID
	if r := c.Get(avatar); r.Status != http.StatusNotFound {
		t.Fatalf("none stored: %d", r.Status)
	}
	r := c.Multipart("POST", "/profile/avatar", "", nil, "me.gif", "image/gif", pngBytes)
	if r.Status != http.StatusSeeOther || r.Location != "/profile" {
		t.Fatalf("%d %q", r.Status, r.Location)
	}
	page := c.Follow(r).Body
	if !strings.Contains(page, "Avatar saved.") || !strings.Contains(page, `src="`+avatar+`"`) || !strings.Contains(page, `data-confirm="Remove your avatar?`) {
		t.Fatal("avatar not shown after upload")
	}
	got := c.Get(avatar)
	if got.Status != 200 || got.Body != string(pngBytes) || got.Header.Get("Content-Type") != "image/png" ||
		got.Header.Get("Cache-Control") != "private, no-cache" || got.Header.Get("X-Content-Type-Options") != "nosniff" ||
		got.Header.Get("Content-Security-Policy") != "sandbox" || got.Header.Get("ETag") == "" {
		t.Fatalf("%d %v", got.Status, got.Header)
	}
	req, _ := http.NewRequest(http.MethodGet, e.Server.URL+avatar, nil)
	req.Header.Set("If-None-Match", got.Header.Get("ETag"))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusNotModified {
		t.Fatalf("revalidate: %d", res.StatusCode)
	}
	// Refusals return to the profile with the message.
	r = c.Multipart("POST", "/profile/avatar", "", nil, "me.svg", "image/svg+xml", []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`))
	if r.Status != http.StatusSeeOther || !strings.Contains(c.Follow(r).Body, "Use a PNG, JPEG or WebP image.") {
		t.Fatalf("%d", r.Status)
	}
	r = c.Multipart("POST", "/profile/avatar", "", nil, "big.png", "image/png", append(append([]byte{}, pngBytes...), make([]byte, 270<<10)...))
	if r.Status != http.StatusSeeOther || !strings.Contains(c.Follow(r).Body, "Use an image of 256 KB or less.") {
		t.Fatalf("%d", r.Status)
	}
	r = c.Multipart("POST", "/profile/avatar", "", nil, "huge.png", "image/png", make([]byte, 400<<10))
	if r.Status != http.StatusRequestEntityTooLarge || strings.Contains(r.Header.Get("Content-Type"), "json") {
		t.Fatalf("over the route limit: %d %q", r.Status, r.Header.Get("Content-Type"))
	}
	r = c.Post("/profile/avatar/delete", nil)
	if r.Status != http.StatusSeeOther || !strings.Contains(c.Follow(r).Body, "Avatar removed.") {
		t.Fatalf("%d", r.Status)
	}
	if r := c.Get(avatar); r.Status != http.StatusNotFound {
		t.Fatalf("after remove: %d", r.Status)
	}
	if r := c.Get("/avatars/nobody"); r.Status != http.StatusNotFound {
		t.Fatalf("unknown actor: %d", r.Status)
	}
}

func TestAPIProfile(t *testing.T) {
	e, c, tok := apiWorld(t)
	var p api.Profile
	decode(t, c.API("GET", "/api/v1/profile", tok, nil).Body, &p)
	if p.Name != "Owner" || p.DisplayName != "" || p.Shown != "Owner" || p.Avatar != nil {
		t.Fatalf("%+v", p)
	}
	r := c.API("PATCH", "/api/v1/profile", tok, map[string]string{"display_name": "Kim"})
	decode(t, r.Body, &p)
	if r.Status != 200 || p.DisplayName != "Kim" || p.Shown != "Kim" || p.Name != "Owner" {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	if r := c.API("PATCH", "/api/v1/profile", tok, map[string]string{"display_name": "x\x01"}); r.Status != 422 {
		t.Fatalf("control: %d %s", r.Status, r.Body)
	}
	if r := c.API("PATCH", "/api/v1/profile", tok, map[string]string{"nickname": "x"}); r.Status != 400 && r.Status != 422 {
		t.Fatalf("unknown field: %d", r.Status)
	}
	r = c.Multipart("PUT", "/api/v1/profile/avatar", tok, nil, "me.webp", "image/png", webpBytes)
	p = api.Profile{}
	decode(t, r.Body, &p)
	if r.Status != 200 || p.Avatar == nil || p.Avatar.URL != "/avatars/"+e.Deps.Actor.ID {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	for _, tc := range []struct {
		name string
		data []byte
		code int
		want string
	}{
		{"me.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), 422, "Use a PNG, JPEG or WebP image."},
		{"big.png", append(append([]byte{}, pngBytes...), make([]byte, 300<<10)...), 413, "Use an image of 256 KB or less."},
	} {
		if r := c.Multipart("PUT", "/api/v1/profile/avatar", tok, nil, tc.name, "image/png", tc.data); r.Status != tc.code || !strings.Contains(r.Body, tc.want) {
			t.Errorf("%s: %d %s", tc.name, r.Status, r.Body)
		}
	}
	r = c.API("DELETE", "/api/v1/profile/avatar", tok, nil)
	p = api.Profile{}
	decode(t, r.Body, &p)
	if r.Status != 200 || p.Avatar != nil {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	if r := c.API("GET", "/api/v1/profile", "", nil); r.Status != 401 {
		t.Fatalf("no token: %d", r.Status)
	}
}

// Review Focus 5: an agent acting through the owner's token (the
// Vaquero-Agent header, which --agent and vaquero mcp --agent send) can't
// edit the owner's profile.
func TestProfileOwnerOnlyAPI(t *testing.T) {
	e, c, tok := apiWorld(t)
	c.Agent = "claude"
	const want = "Only the owner edits the owner's profile."
	for _, r := range []*webtest.Response{
		c.API("PATCH", "/api/v1/profile", tok, map[string]string{"display_name": "Claude"}),
		c.Multipart("PUT", "/api/v1/profile/avatar", tok, nil, "me.png", "image/png", pngBytes),
		c.API("DELETE", "/api/v1/profile/avatar", tok, nil),
	} {
		if r.Status != 422 || !strings.Contains(r.Body, want) {
			t.Errorf("%d %s", r.Status, r.Body)
		}
	}
	if a, _ := e.Deps.R.GetActor(bg, e.Deps.Actor.ID); a.DisplayName != "" {
		t.Fatalf("%+v", a)
	}
}

// VAQ-149: after a rename every place a name shows reads the new one,
// past history included, because names are looked up by actor id.
func TestDisplayNameEverywhere(t *testing.T) {
	e, c, tok := apiWorld(t)
	assign(t, e, "VAQ-1", e.Deps.Actor.ID)
	if _, err := e.Deps.Core.UpdateProfile(bg, e.Deps.Actor.ID, new("Kim Lee")); err != nil {
		t.Fatal(err)
	}
	page := c.Get("/t/VAQ-1").Body
	for _, want := range []string{
		`<span class="font-medium text-ink">Kim Lee</span>`, // the history's lines and the comment
		`data-assignee="Kim Lee"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("ticket page lacks %s", want)
		}
	}
	if strings.Contains(page, `<span class="font-medium text-ink">Owner</span>`) {
		t.Error("ticket page still names Owner")
	}
	if board := c.Get("/p/VAQ/board").Body; !strings.Contains(board, "Kim Lee") {
		t.Error("board card lacks the name")
	}
	var tk api.Ticket
	decode(t, c.API("GET", "/api/v1/tickets/VAQ-1", tok, nil).Body, &tk)
	if tk.Assignee != "Kim Lee" || len(tk.Comments) != 1 || tk.Comments[0].Author != "Kim Lee" {
		t.Fatalf("%+v", tk)
	}
	// Write answers name the author by the shown name too (fix round 1):
	// a comment created and edited, through the API and MCP, and an
	// upload's uploader.
	var cm api.Comment
	r := c.API("POST", "/api/v1/tickets/VAQ-1/comments", tok, map[string]string{"body": "Saddled."})
	decode(t, r.Body, &cm)
	if r.Status != 201 || cm.Author != "Kim Lee" {
		t.Fatalf("comment create: %d %s", r.Status, r.Body)
	}
	r = c.API("PATCH", "/api/v1/comments/"+cm.ID, tok, map[string]string{"body": "Saddled up."})
	decode(t, r.Body, &cm)
	if r.Status != 200 || cm.Author != "Kim Lee" {
		t.Fatalf("comment edit: %d %s", r.Status, r.Body)
	}
	cs := mcpConnect(t, e, tok, "")
	if res, text := mcpCall(t, cs, "create_comment", map[string]any{"key": "VAQ-1", "body": "Through MCP."}); res.IsError || !strings.Contains(text, `"author": "Kim Lee"`) && !strings.Contains(text, `"author":"Kim Lee"`) {
		t.Fatalf("mcp create_comment: %s", text)
	}
	var att api.Attachment
	r = c.UploadAs("/api/v1/tickets/VAQ-1/attachments", tok, "notes.txt", "text/plain", []byte("hay"))
	decode(t, r.Body, &att)
	if r.Status != 201 || att.UploadedBy != "Kim Lee" {
		t.Fatalf("upload: %d %s", r.Status, r.Body)
	}
	var me struct{ Actor map[string]string }
	body := c.API("GET", "/api/v1/me", tok, nil).Body
	decode(t, body, &me)
	if a := me.Actor; a["name"] != "Owner" || a["display_name"] != "Kim Lee" || a["shown_name"] != "Kim Lee" || a["kind"] != "human" {
		t.Fatalf("me: %s", body)
	}
	var evs []api.Event
	decode(t, c.API("GET", "/api/v1/events", tok, nil).Body, &evs)
	for _, ev := range evs {
		if ev.Actor != "Kim Lee" {
			t.Fatalf("event by %q", ev.Actor)
		}
	}
}

// Ruling P-R3: the assignee is taken by name or display name.
func TestAssigneeByDisplayName(t *testing.T) {
	e, c, tok := apiWorld(t)
	if _, err := e.Deps.Core.UpdateProfile(bg, e.Deps.Actor.ID, new("Kim")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Kim", "kim", "KIM", "Owner"} {
		r := c.API("PATCH", "/api/v1/tickets/VAQ-1", tok, map[string]string{"assignee": name})
		var tk api.Ticket
		decode(t, r.Body, &tk)
		if r.Status != 200 || tk.Assignee != "Kim" {
			t.Fatalf("%s: %d %s", name, r.Status, r.Body)
		}
		assign(t, e, "VAQ-1", "")
	}
	assign(t, e, "VAQ-1", e.Deps.Actor.ID)
	var list []api.TicketSummary
	r := c.API("GET", "/api/v1/projects/VAQ/tickets?assignee=Kim", tok, nil)
	decode(t, r.Body, &list)
	if r.Status != 200 || len(list) != 1 || list[0].Key != "VAQ-1" {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
}

// Avatars show beside names: the owner's image once uploaded, an agent's
// monogram tile otherwise, on board cards and in the history.
func TestAvatarsBesideNames(t *testing.T) {
	e, c, tok := apiWorld(t)
	c.Agent = "claude"
	if r := c.API("PATCH", "/api/v1/tickets/VAQ-2", tok, map[string]string{"assignee": "claude"}); r.Status != 200 {
		t.Fatalf("%d %s", r.Status, r.Body)
	}
	c.Agent = ""
	if _, err := e.Deps.Core.SetAvatar(bg, e.Deps.Actor.ID, "", pngBytes); err != nil {
		t.Fatal(err)
	}
	board := c.Get("/p/VAQ/board").Body
	if !strings.Contains(board, `data-avatar-monogram="CL"`) {
		t.Error("board card lacks the agent's tile")
	}
	page := c.Get("/t/VAQ-2").Body
	if !strings.Contains(page, `src="/avatars/`+e.Deps.Actor.ID+`"`) || !strings.Contains(page, `data-avatar-monogram="CL"`) {
		t.Error("history lacks the avatars")
	}
	agent, err := e.Deps.R.GetActorByKindName(bg, gen.GetActorByKindNameParams{Kind: "agent", Name: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	tile := "bg-tile-" + string(rune('0'+core.TileColor(agent.ID)))
	if !strings.Contains(page, tile) || !strings.Contains(c.Get("/t/VAQ-2").Body, tile) {
		t.Errorf("agent tile %s missing", tile)
	}
}

// assign sets ticket key's assignee to actor id ("" for none) through core.
func assign(t *testing.T, e *webtest.Env, key, id string) {
	t.Helper()
	k, _ := core.ParseTicketKey(key)
	p, err := e.Deps.R.GetProjectByKey(bg, k.Project)
	if err != nil {
		t.Fatal(err)
	}
	tk, err := e.Deps.R.GetTicketByNumber(bg, gen.GetTicketByNumberParams{ProjectID: p.ID, Number: k.Number})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Deps.Core.PatchTicket(bg, e.Deps.Actor.ID, tk.ID, core.TicketPatch{AssigneeID: &id}, 0); err != nil {
		t.Fatal(err)
	}
}

// The assignee's display name is matched in NFC with case folding, as
// tags are (fix round 1).
func TestAssigneeDisplayNameFolded(t *testing.T) {
	e, c, tok := apiWorld(t)
	if _, err := e.Deps.Core.UpdateProfile(bg, e.Deps.Actor.ID, new("Jos\u00e9")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Jose\u0301", "JOS\u00c9"} {
		r := c.API("PATCH", "/api/v1/tickets/VAQ-1", tok, map[string]string{"assignee": name})
		var tk api.Ticket
		decode(t, r.Body, &tk)
		if r.Status != 200 || tk.Assignee != "Jos\u00e9" {
			t.Fatalf("%q: %d %s", name, r.Status, r.Body)
		}
		assign(t, e, "VAQ-1", "")
	}
}

// The themed 413 page for an oversized profile form shows the owner's
// face in its header, like every other page (fix round 1).
func TestTooLargePageShowsTheOwner(t *testing.T) {
	e := webtest.New(t)
	if _, err := e.Deps.Core.UpdateProfile(bg, e.Deps.Actor.ID, new("Kim Lee")); err != nil {
		t.Fatal(err)
	}
	r := e.Client().Post("/profile", url.Values{"display_name": {strings.Repeat("x", 2<<20)}})
	if r.Status != http.StatusRequestEntityTooLarge || !strings.Contains(r.Body, `data-avatar-monogram="KL"`) {
		t.Fatalf("%d", r.Status)
	}
}
