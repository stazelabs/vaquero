# Phase 1a: Data model, operations layer and basic pages — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** In local mode, a person can create projects, tickets and sub-tickets, give them statuses, priorities, tags, milestones and links, hold a running comment thread, and see an activity feed, through plain server-rendered pages. Every change is made by `internal/core` and leaves one event.

**Architecture:** One additive migration adds the tracker tables. `internal/core` is the only package that writes them. Each operation opens one writer transaction, validates, writes, appends exactly one `events` row and commits. Handlers read through the reader queries (`Deps.R`) and write only through `Deps.Core`; `Deps.Q` is removed so that rule is structural. The pages are deliberately plain (forms and lists). Phase 1b replaces the project page with a drag-and-drop board and adds the list view with filters, search, live updates and keyboard shortcuts.

**Tech Stack:** As Phase 0, plus `github.com/yuin/goldmark v1.8.2` (the version starfish uses) and `github.com/microcosm-cc/bluemonday v1.0.27` for ticket and comment markdown.

**Spec:** `docs/superpowers/specs/2026-11-18-vaquero-design.md`. Phase 0 plan: `docs/superpowers/plans/2026-11-18-phase-0-foundation.md`.

## Global Constraints

- Everything in CLAUDE.md "Hard invariants" applies. In particular: module `github.com/stazelabs/vaquero`, `CGO_ENABLED=0`, IDs only from `idgen.New()`, instants only through `timefmt`, one writer connection, `gen.New(tx)` inside transactions, no inline script or `style=`, theme tokens only, generated files committed.
- Migrations are append-only and forward-only. This plan adds `0002_tracker.sql` and never edits `0001_init.sql`.
- Only `internal/core` writes the tracker tables. Handlers write only through `Deps.Core`.
- Every successful core operation appends exactly one row to `events`, in the same transaction as the change. A refused operation (validation or not-found error) writes nothing.
- Core nouns stay plain: project, ticket, status, tag, comment, link, milestone, actor.
- The spec's status categories are fixed: `backlog`, `todo`, `in_progress`, `in_review`, `done`, `cancelled`. A status's name and order are editable; its category is not.
- Project keys are immutable once created (branch names and imports depend on them).
- In local mode every change is made by the local owner actor.
- Copy follows `.claude/rules/copy-voice.md`: sentence case, no exclamation points, buttons name the action.
- Commit messages: `{Area}: {summary}`, ending with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- Gate before every commit: `make gen && make fmt && make vet && make lint && make test`. `make ci` at the end of each task.
- Deferred work found along the way goes into `docs/BACKLOG.md`.

## Review Focus

1. **Two tickets are created in the same project at the same moment** (two agents later, two browser tabs now). They must get different numbers, never a duplicate-key 500. (Test in Task 3.)
2. **Someone sets a ticket's parent to its own sub-ticket, to itself, or to a ticket in another project.** Each is refused with a plain message naming the problem, and nothing changes. (Test in Task 3.)
3. **A ticket body or comment contains `<script>`, `[x](javascript:alert(1))`, `<javascript:alert(1)>` or an `<img onerror>`.** It renders as inert text or a harmless link, never as script. (Test in Task 5.)
4. **Someone types `vaq-12`, `VAQ-012`, `VAQ-99999` or `nonsense` into a ticket URL or the link form.** Case and leading-zero variants reach the ticket (URLs redirect to the canonical key); unknown or malformed keys give a plain message, never a 500. (Tests in Tasks 3 and 7.)
5. **An operation fails part-way** (for example, a valid ticket with a tag name that breaks a constraint). No ticket, tag or event row is left behind. (Test in Task 2, as a property over every operation.)

---

### Task 1: Tracker schema and queries

**Files:**
- Create: `internal/db/migrations/0002_tracker.sql`
- Create: `internal/db/queries/actors.sql`, `projects.sql`, `statuses.sql`, `tickets.sql`, `tags.sql`, `comments.sql`, `links.sql`, `milestones.sql`, `events.sql`
- Create: `internal/db/tracker_test.go`
- Generated: `internal/db/gen/`

**Interfaces:**
- Produces these tables (all `id TEXT PRIMARY KEY` ULIDs unless noted; all instants `TEXT` via timefmt; `-- +goose Down` is `SELECT 1;`):

```sql
actors      (id, kind TEXT NOT NULL CHECK (kind IN ('human','agent')),
             name TEXT NOT NULL CHECK (length(trim(name)) BETWEEN 1 AND 80),
             created_at TEXT NOT NULL,
             UNIQUE (kind, name))
projects    (id, key TEXT NOT NULL UNIQUE CHECK (length(key) BETWEEN 2 AND 10 AND key GLOB '[A-Z]*' AND key NOT GLOB '*[^A-Z0-9]*'),
             name TEXT NOT NULL CHECK (length(trim(name)) BETWEEN 1 AND 120),
             description TEXT NOT NULL DEFAULT '' CHECK (length(description) <= 20000),
             next_number INTEGER NOT NULL DEFAULT 1,
             created_at, updated_at TEXT NOT NULL, archived_at TEXT)
statuses    (id, project_id TEXT NOT NULL REFERENCES projects(id),
             name TEXT NOT NULL CHECK (length(trim(name)) BETWEEN 1 AND 40),
             category TEXT NOT NULL CHECK (category IN ('backlog','todo','in_progress','in_review','done','cancelled')),
             position INTEGER NOT NULL,
             UNIQUE (project_id, name))
milestones  (id, project_id TEXT NOT NULL REFERENCES projects(id),
             name TEXT NOT NULL CHECK (length(trim(name)) BETWEEN 1 AND 80),
             description TEXT NOT NULL DEFAULT '', due_on TEXT,  -- YYYY-MM-DD
             created_at TEXT NOT NULL, closed_at TEXT,
             UNIQUE (project_id, name))
tickets     (id, project_id TEXT NOT NULL REFERENCES projects(id), number INTEGER NOT NULL,
             title TEXT NOT NULL CHECK (length(trim(title)) BETWEEN 1 AND 200),
             body TEXT NOT NULL DEFAULT '' CHECK (length(body) <= 100000),
             status_id TEXT NOT NULL REFERENCES statuses(id),
             priority INTEGER NOT NULL DEFAULT 0 CHECK (priority BETWEEN 0 AND 4), -- 0 none, 1 urgent, 2 high, 3 medium, 4 low
             assignee_id TEXT REFERENCES actors(id),
             parent_id TEXT REFERENCES tickets(id),
             milestone_id TEXT REFERENCES milestones(id),
             position REAL NOT NULL,          -- order within a status column; Phase 1b drags it
             created_by TEXT NOT NULL REFERENCES actors(id),
             created_at, updated_at TEXT NOT NULL,
             UNIQUE (project_id, number))
tags        (id, name TEXT NOT NULL UNIQUE COLLATE NOCASE CHECK (length(trim(name)) BETWEEN 1 AND 40), created_at TEXT NOT NULL)
ticket_tags (ticket_id TEXT NOT NULL REFERENCES tickets(id), tag_id TEXT NOT NULL REFERENCES tags(id), PRIMARY KEY (ticket_id, tag_id))
comments    (id, ticket_id TEXT NOT NULL REFERENCES tickets(id), author_id TEXT NOT NULL REFERENCES actors(id),
             body TEXT NOT NULL CHECK (length(trim(body)) BETWEEN 1 AND 20000),
             created_at, updated_at TEXT NOT NULL, deleted_at TEXT)
ticket_links(id, from_id TEXT NOT NULL REFERENCES tickets(id), to_id TEXT NOT NULL REFERENCES tickets(id),
             kind TEXT NOT NULL CHECK (kind IN ('blocks','relates','duplicates')),
             created_at TEXT NOT NULL, CHECK (from_id <> to_id), UNIQUE (from_id, to_id, kind))
events      (seq INTEGER PRIMARY KEY AUTOINCREMENT, at TEXT NOT NULL,
             actor_id TEXT NOT NULL REFERENCES actors(id),
             kind TEXT NOT NULL, project_id TEXT REFERENCES projects(id), ticket_id TEXT REFERENCES tickets(id),
             payload TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(payload)))
```

  Indexes: `tickets (project_id, status_id, position)`, `tickets (parent_id)`, `tickets (milestone_id)`, `comments (ticket_id, created_at)`, `ticket_links (to_id)`, `events (ticket_id, seq)`, `events (project_id, seq)`.

- Produces sqlc queries (names are the contract; the SQL is the obvious one):
  - actors: `CreateActor`, `GetActor`, `GetActorByKindName`, `ListActors`.
  - projects: `CreateProject`, `GetProject`, `GetProjectByKey`, `ListProjects` (active first by name, then archived), `UpdateProject`, `SetProjectArchived`, `NextTicketNumber :one` (`UPDATE projects SET next_number = next_number + 1 WHERE id = ? RETURNING next_number - 1`).
  - statuses: `CreateStatus`, `GetStatus`, `ListStatuses` (by position), `RenameStatus`, `SetStatusPosition`, `DeleteStatus`, `CountTicketsInStatus`, `CountStatusesInCategory`.
  - milestones: `CreateMilestone`, `GetMilestone`, `ListMilestones`, `UpdateMilestone`, `SetMilestoneClosed`.
  - tickets: `CreateTicket`, `GetTicket`, `GetTicketByNumber` (project_id, number), `ListTicketsByProject` (joined with status name and category, ordered by status position then ticket position), `ListSubTickets`, `UpdateTicket` (all editable columns), `MaxPositionInStatus`, `ListAncestorIDs` (a recursive CTE up `parent_id`).
  - tags: `UpsertTag :one` (`INSERT … ON CONFLICT(name) DO UPDATE SET name = name RETURNING *`), `ListTags`, `ListTicketTags`, `AddTicketTag`, `RemoveTicketTag`.
  - comments: `CreateComment`, `GetComment`, `ListComments` (not deleted, oldest first), `UpdateComment`, `SoftDeleteComment`.
  - links: `CreateLink`, `GetLink`, `DeleteLink`, `ListLinksFrom`, `ListLinksTo` (each joined with the other ticket's project key, number, title and status category).
  - events: `AppendEvent :one` (returns `seq`), `ListEvents` (newest first, `LIMIT ?`), `ListTicketEvents`, `CountEvents`.

- [ ] **Step 1: Write the failing tests** in `internal/db/tracker_test.go`

```go
func TestTrackerTablesExist(t *testing.T) {
	d := dbtest.New(t)
	for _, tbl := range []string{"actors", "projects", "statuses", "milestones", "tickets", "tags", "ticket_tags", "comments", "ticket_links", "events"} {
		var n int
		if err := d.R.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, tbl).Scan(&n); err != nil || n != 1 {
			t.Fatalf("table %s: %d %v", tbl, n, err)
		}
	}
}

func TestProjectKeyCheck(t *testing.T) {
	d := dbtest.New(t)
	for key, ok := range map[string]bool{"VAQ": true, "A1": true, "ABCDEFGHIJ": true, "A": false, "vaq": false, "1AB": false, "AB-C": false, "ABCDEFGHIJK": false} {
		_, err := d.W.Exec(`INSERT INTO projects (id, key, name, created_at, updated_at) VALUES (?, ?, 'P', 't', 't')`, idgen.New(), key)
		if (err == nil) != ok { t.Errorf("key %q: err=%v, want ok=%v", key, err, ok) }
	}
}

func TestTicketNumberUniquePerProject(t *testing.T) { /* two tickets with the same (project_id, number) fail with a UNIQUE error */ }
func TestTagNamesCaseInsensitive(t *testing.T)       { /* UpsertTag("Bug") then UpsertTag("bug") return the same id */ }
func TestEventPayloadMustBeJSON(t *testing.T)        { /* AppendEvent with payload "not json" fails */ }
```

- [ ] **Step 2:** Run `go test ./internal/db/...` — Expected: FAIL (tables and queries missing).
- [ ] **Step 3:** Write `0002_tracker.sql` and the query files exactly as Interfaces says. Run `make sqlc`.
- [ ] **Step 4:** Run `go test ./internal/db/...` — Expected: PASS.
- [ ] **Step 5: Commit** — `"DB: tracker schema and queries"`

---

### Task 2: `internal/core` foundation: errors, transactions, events, actors

**Files:**
- Create: `internal/core/core.go`, `errors.go`, `events.go`, `actors.go`, `core_test.go`, `coretest_test.go` (shared test helpers)

**Interfaces:**
- Produces:

```go
package core

type Core struct {
	DB  *sql.DB          // writer pool
	Now func() time.Time
}
func New(db *sql.DB, now func() time.Time) *Core

// ErrNotFound: the named thing does not exist. Handlers redirect with a flash.
var ErrNotFound = errors.New("not found")

// Invalid is a refused input: Fields maps a form field name to a sentence-case
// message ("Enter a title."). Error() joins the messages.
type Invalid struct{ Fields map[string]string }
func (e *Invalid) Error() string
func invalid(field, msg string) *Invalid

// Event kinds (the payload shapes are in events.go's doc comment).
const (
	EvProjectCreated = "project.created"; EvProjectUpdated = "project.updated"
	EvProjectArchived = "project.archived"; EvProjectRestored = "project.restored"
	EvStatusCreated = "status.created"; EvStatusUpdated = "status.updated"
	EvStatusMoved = "status.moved"; EvStatusDeleted = "status.deleted"
	EvMilestoneCreated = "milestone.created"; EvMilestoneUpdated = "milestone.updated"
	EvMilestoneClosed = "milestone.closed"; EvMilestoneReopened = "milestone.reopened"
	EvTicketCreated = "ticket.created"; EvTicketUpdated = "ticket.updated"
	EvCommentCreated = "comment.created"; EvCommentUpdated = "comment.updated"; EvCommentDeleted = "comment.deleted"
	EvLinkCreated = "link.created"; EvLinkDeleted = "link.deleted"
	EvActorCreated = "actor.created"
)

// Change is one field's before and after, in an *.updated payload: {"changes":{"title":{"from":"a","to":"b"}}}.
type Change struct{ From, To any }

func (c *Core) EnsureOwner(ctx context.Context) (gen.Actor, error) // the human actor named "Owner"; created (with an actor.created event by itself) on first call, returned on later calls
```

- Unexported, used by every later core task: `func (c *Core) tx(ctx context.Context, fn func(q *gen.Queries) error) error` (BEGIN on the writer, `gen.New(tx)`, commit or roll back) and `func (c *Core) emit(ctx context.Context, q *gen.Queries, actorID, kind, projectID, ticketID string, payload any) error` (empty IDs stored as NULL; payload marshalled with `encoding/json`; nil payload is `{}`).
- Test helper in `coretest_test.go` (package `core_test`), reused by Tasks 3–4: `func newCore(t *testing.T) (*core.Core, dbtest.DB, gen.Actor)` returning a core on `dbtest.New(t)` with a fixed clock at `2026-11-18T12:00:00Z` and the owner actor. `func eventCount(t *testing.T, d dbtest.DB) int64`.

- [ ] **Step 1: Write the failing tests** in `core_test.go`

```go
func TestEnsureOwnerIsIdempotent(t *testing.T) {
	c, d, _ := newCore(t) // newCore already called EnsureOwner once
	before := eventCount(t, d)
	a, err := c.EnsureOwner(context.Background())
	if err != nil || a.Kind != "human" || a.Name != "Owner" { t.Fatalf("%+v %v", a, err) }
	if eventCount(t, d) != before { t.Fatal("second EnsureOwner wrote an event") }
}

func TestInvalidErrorJoinsFields(t *testing.T) {
	e := &core.Invalid{Fields: map[string]string{"title": "Enter a title.", "key": "Use 2 to 10 capital letters or digits."}}
	if !strings.Contains(e.Error(), "Enter a title.") || !strings.Contains(e.Error(), "Use 2 to 10") { t.Fatal(e.Error()) }
}
```

- [ ] **Step 2:** Run `go test ./internal/core` — Expected: FAIL, package missing.
- [ ] **Step 3:** Implement the Interfaces. Package doc comment states the rule: only core writes the tracker tables, one event per successful operation, in the same transaction.
- [ ] **Step 4:** Run `go test ./internal/core` — Expected: PASS.
- [ ] **Step 5:** Add the property test that later tasks extend (Review Focus 5). In `core_test.go`:

```go
// Every operation in ops either succeeds and appends exactly one event, or
// fails and leaves every tracker table unchanged. Tasks 3 and 4 add their
// operations to ops, each with a good and a bad input.
var ops []coreOp // type coreOp struct{ name string; good, bad func(*core.Core, fixture) error }

func TestEveryOperationEmitsOneEventOrNothing(t *testing.T) {
	for _, op := range ops {
		t.Run(op.name, func(t *testing.T) {
			c, d, f := newFixture(t) // a project "VAQ" with default statuses, two tickets, one tag, one milestone
			snap := snapshot(t, d)   // row counts of every tracker table
			if err := op.bad(c, f); err == nil { t.Fatal("bad input accepted") }
			if got := snapshot(t, d); !maps.Equal(got, snap) { t.Fatalf("bad input changed rows: %v → %v", snap, got) }
			if err := op.good(c, f); err != nil { t.Fatal(err) }
			if got := snapshot(t, d)["events"]; got != snap["events"]+1 { t.Fatalf("events %d → %d", snap["events"], got) }
		})
	}
}
```

  Until Task 3, `ops` is empty and `newFixture` may `t.Skip` when it is; Task 3 implements `newFixture`.
- [ ] **Step 6: Commit** — `"Core: transactions, errors, events, owner actor"`

---

### Task 3: Core operations: projects, statuses, milestones, tickets, tags

**Files:**
- Create: `internal/core/projects.go`, `statuses.go`, `milestones.go`, `tickets.go`, `keys.go`, and tests `projects_test.go`, `tickets_test.go`, `keys_test.go`
- Modify: `internal/core/core_test.go` (`ops` entries, `newFixture`, `snapshot`)

**Interfaces:**
- Produces:

```go
// keys.go
type TicketKey struct{ Project string; Number int64 }
func ParseTicketKey(s string) (TicketKey, bool) // trims, uppercases the project part, accepts leading zeros ("vaq-012" → {"VAQ",12}); false for anything else
func (k TicketKey) String() string              // "VAQ-12"

// projects.go
type ProjectInput struct{ Key, Name, Description string }
func (c *Core) CreateProject(ctx context.Context, by string, in ProjectInput) (gen.Project, error) // seeds the six default statuses
func (c *Core) UpdateProject(ctx context.Context, by, projectID string, name, description string) (gen.Project, error) // key never changes
func (c *Core) SetProjectArchived(ctx context.Context, by, projectID string, archived bool) error

// statuses.go
func (c *Core) CreateStatus(ctx context.Context, by, projectID, name, category string) (gen.Status, error) // appended at the end
func (c *Core) RenameStatus(ctx context.Context, by, statusID, name string) error
func (c *Core) MoveStatus(ctx context.Context, by, statusID string, delta int) error // -1 up, +1 down; swaps positions; a no-op at either end is refused as Invalid
func (c *Core) DeleteStatus(ctx context.Context, by, statusID string) error           // refused while any ticket uses it, or when it is the last of its category

// milestones.go
type MilestoneInput struct{ Name, Description, DueOn string } // DueOn "" or YYYY-MM-DD
func (c *Core) CreateMilestone(ctx context.Context, by, projectID string, in MilestoneInput) (gen.Milestone, error)
func (c *Core) UpdateMilestone(ctx context.Context, by, milestoneID string, in MilestoneInput) error
func (c *Core) SetMilestoneClosed(ctx context.Context, by, milestoneID string, closed bool) error

// tickets.go
type TicketInput struct {
	Title, Body string
	StatusID    string   // "" means the project's first status in category todo
	Priority    int64    // 0–4
	AssigneeID  string   // "" for none
	ParentID    string   // "" for none
	MilestoneID string   // "" for none
	Tags        []string // names; created as needed; trimmed, blanks and duplicates (case-insensitive) dropped
}
func (c *Core) CreateTicket(ctx context.Context, by, projectID string, in TicketInput) (gen.Ticket, error)
func (c *Core) UpdateTicket(ctx context.Context, by, ticketID string, in TicketInput) (gen.Ticket, error) // replaces every field, including the tag set
func (c *Core) ResolveTicket(ctx context.Context, q *gen.Queries, key TicketKey) (gen.Ticket, gen.Project, error) // ErrNotFound when either is missing; used by handlers with d.R
```

- Default statuses for a new project, in order: `Backlog` (backlog), `Todo` (todo), `In progress` (in_progress), `In review` (in_review), `Done` (done), `Cancelled` (cancelled).
- Validation messages (`Invalid.Fields` keys are the form field names):
  - `key`: "Use 2 to 10 capital letters or digits, starting with a letter." / "That key is taken."
  - `name`: "Enter a name."
  - `title`: "Enter a title." / "Keep the title under 200 characters."
  - `status`: "Choose a status from this project."
  - `priority`: "Choose a priority."
  - `parent`: "A ticket can't be its own parent." / "That ticket is a sub-ticket of this one." / "The parent must be in the same project."
  - `milestone`: "Choose a milestone from this project."
  - `due_on`: "Use a date like 2026-12-31."
  - `category`: "Choose a category."
  - status deletion: "Move this status's tickets first." / "A project needs at least one status in each category."
- A ticket's new `position` is `MaxPositionInStatus + 1024` (1024 for an empty status). `UpdateTicket` keeps the position unless the status changed, then it goes to the end of the new status.
- `ticket.updated` payload lists only changed fields, with tags as sorted name lists. An update that changes nothing succeeds and writes no event (the one exception to one-event-per-operation; the property test's `good` input must change something).

- [ ] **Step 1: Write the failing tests**

```go
// keys_test.go
func TestParseTicketKey(t *testing.T) {
	for in, want := range map[string]string{"VAQ-12": "VAQ-12", "vaq-12": "VAQ-12", " VAQ-012 ": "VAQ-12"} {
		k, ok := core.ParseTicketKey(in)
		if !ok || k.String() != want { t.Errorf("%q → %v %v", in, k, ok) }
	}
	for _, bad := range []string{"", "VAQ", "VAQ-", "VAQ-0", "VAQ-x", "-12", "VAQ-12-3", "nonsense"} {
		if _, ok := core.ParseTicketKey(bad); ok { t.Errorf("%q accepted", bad) }
	}
}

// projects_test.go
func TestCreateProjectSeedsStatuses(t *testing.T) { /* six statuses, names and categories in the order above */ }
func TestCreateProjectRefusesBadOrTakenKey(t *testing.T) { /* "vaq" → Invalid{key}; second "VAQ" → Invalid{key:"That key is taken."} */ }
func TestDeleteStatusRules(t *testing.T) { /* refused while in use; refused for the only "done"; allowed for an extra empty one */ }

// tickets_test.go
func TestTicketNumbersIncrease(t *testing.T) { /* VAQ-1, VAQ-2, VAQ-3 */ }

// Review Focus 1
func TestConcurrentCreatesGetDistinctNumbers(t *testing.T) {
	c, _, f := newFixture(t)
	var wg sync.WaitGroup
	nums := make(chan int64, 20)
	for range 20 {
		wg.Go(func() {
			tk, err := c.CreateTicket(context.Background(), f.Owner.ID, f.Project.ID, core.TicketInput{Title: "t"})
			if err != nil { t.Error(err); return }
			nums <- tk.Number
		})
	}
	wg.Wait(); close(nums)
	seen := map[int64]bool{}
	for n := range nums { if seen[n] { t.Fatalf("duplicate number %d", n) }; seen[n] = true }
	if len(seen) != 20 { t.Fatalf("%d tickets", len(seen)) }
}

// Review Focus 2
func TestParentRules(t *testing.T) {
	// a → b → c (c's parent is b, b's parent is a)
	// UpdateTicket(a, Parent=c) → Invalid{parent:"That ticket is a sub-ticket of this one."}
	// UpdateTicket(a, Parent=a) → Invalid{parent:"A ticket can't be its own parent."}
	// UpdateTicket(a, Parent=<ticket in project OTHER>) → Invalid{parent:"The parent must be in the same project."}
	// after all three, a.ParentID is still empty and the events count is unchanged
}

func TestUpdateTicketPayloadListsChanges(t *testing.T) { /* title and tags changed → payload {"changes":{"title":{…},"tags":{"from":["bug"],"to":["bug","ui"]}}} */ }
func TestUpdateWithNoChangeWritesNoEvent(t *testing.T) {}
func TestStatusChangeMovesToEndOfColumn(t *testing.T) {}
func TestResolveTicket(t *testing.T) { /* VAQ-1 found; VAQ-99 and NOPE-1 → ErrNotFound */ }
```

  Add `ops` entries for every operation in this task, each with a good input and a bad one (for example `CreateTicket` with an empty title; `DeleteStatus` on a status in use; `CreateProject` with key "x").
- [ ] **Step 2:** Run `go test ./internal/core` — Expected: FAIL to compile.
- [ ] **Step 3:** Implement. Parent cycle check: refuse when the proposed parent's `ListAncestorIDs` contains the ticket's own id, or the proposed parent is the ticket itself. Number allocation uses `NextTicketNumber` inside the transaction.
- [ ] **Step 4:** Run `go test -race ./internal/core` — Expected: PASS.
- [ ] **Step 5: Commit** — `"Core: projects, statuses, milestones, tickets, tags"`

---

### Task 4: Core operations: comments and links

**Files:**
- Create: `internal/core/comments.go`, `links.go`, `comments_test.go`, `links_test.go`
- Modify: `internal/core/core_test.go` (`ops` entries)

**Interfaces:**
- Produces:

```go
func (c *Core) AddComment(ctx context.Context, by, ticketID, body string) (gen.Comment, error)
func (c *Core) EditComment(ctx context.Context, by, commentID, body string) error   // only the author may edit: Invalid{body:"You can only edit your own comments."}
func (c *Core) DeleteComment(ctx context.Context, by, commentID string) error       // soft delete; only the author
func (c *Core) AddLink(ctx context.Context, by, fromTicketID, kind string, to TicketKey) (gen.TicketLink, error)
func (c *Core) RemoveLink(ctx context.Context, by, linkID string) error
```

- Messages: `body`: "Write a comment." / "Keep comments under 20,000 characters."; `to`: "No ticket VAQ-99." (with the key as typed, canonicalised) / "A ticket can't link to itself." / "Those tickets are already linked that way."; `kind`: "Choose blocks, relates to or duplicates."
- Comment and link events carry the ticket id (`AddLink`: the from ticket). Their payloads: comment → `{"comment_id"}`; link → `{"link_id","kind","to":"VAQ-7"}`.

- [ ] **Step 1: Write the failing tests:** `TestAddAndEditComment`, `TestOnlyAuthorEditsComment` (a second actor created with `CreateActor` in the test is refused), `TestDeletedCommentsAreHidden`, `TestAddLinkByKey` (accepts `vaq-2`), `TestLinkRefusals` (self, unknown key, duplicate), `TestLinksCrossProjects` (allowed). Add `ops` entries for all five operations.
- [ ] **Step 2:** Run `go test ./internal/core` — Expected: FAIL to compile.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run `go test -race ./internal/core` — Expected: PASS.
- [ ] **Step 5: Commit** — `"Core: comments and links"`

---

### Task 5: Web plumbing: core in Deps, flash, markdown, form components

**Files:**
- Modify: `internal/web/handlers/deps.go`, `render.go`, `cmd/vaquero/serve.go`, `internal/web/webtest/webtest.go`, `internal/web/templates/layout.templ`, `internal/web/static/js/app.js`
- Create: `internal/web/flash/flash.go`, `flash_test.go`
- Create: `internal/markdown/markdown.go`, `markdown_test.go`
- Create: `internal/web/templates/components.templ` (port from startcase, plus `Select`)
- Create: `internal/web/handlers/handlers_test.go` (source-scan test)

**Interfaces:**
- `handlers.Deps` becomes `{Cfg, DB *sql.DB, R *gen.Queries, Core *core.Core, Actor gen.Actor, Logger, Now}`. `Q` is removed. `Actor` is the local owner, from `EnsureOwner` at boot in `serve` and in `webtest.New`.
- `flash` (cookie-based; local mode has no sessions yet):

```go
type Kind string // Success, Error
type Flash struct{ Kind Kind; Message string }
func Put(w http.ResponseWriter, kind Kind, msg string)          // cookie vq_flash: base64url(JSON), Path=/, HttpOnly, SameSite=Lax, MaxAge 60
func Pop(w http.ResponseWriter, r *http.Request) (Flash, bool)  // reads and expires the cookie; a malformed cookie is ignored
func WithCurrent(ctx context.Context, f Flash) context.Context
func Current(ctx context.Context) (Flash, bool)
```

- `render` pops the flash for non-htmx requests and puts it on the context; `Shell` shows it as a toast (`role="status"`), which `app.js` fades after 5 s and closes on its × button.
- Handler helpers in `render.go`: `serverError(w, r, d, err)` (restored now that handlers can fail), `redirectWithFlash(w, r, kind, msg, to string)` (303), `invalidOrError(w, r, d, err error, rerender func(fields map[string]string))` (a `*core.Invalid` re-renders the form with 422 and its field messages; `core.ErrNotFound` redirects to `/` with "That no longer exists."; anything else is `serverError`).
- `markdown`:

```go
func Render(src string) template.HTML // goldmark v1.8.2 (raw HTML escaped, extension.GFM) then bluemonday.UGCPolicy() with RequireNoFollowOnLinks; links open normally
```

- Components (`components.templ`): `TextInput(p InputProps)`, `TextArea(p InputProps)`, `FieldError(msg string)` ported from startcase; add `Select(p SelectProps)` with `SelectProps{Name, Label, Options []Option{Value, Label string}, Selected, Error string}`. Field ids are `f-` + name.

- [ ] **Step 1: Write the failing tests**

```go
// markdown_test.go — Review Focus 3
func TestRenderIsInert(t *testing.T) {
	for _, src := range []string{
		"<script>alert(1)</script>",
		"[x](javascript:alert(1))",
		"<javascript:alert(1)>",
		`<img src=x onerror="alert(1)">`,
		"[x](JaVaScRiPt:alert(1))",
		"[x](data:text/html;base64,PHNjcmlwdD4=)",
	} {
		out := strings.ToLower(string(markdown.Render(src)))
		for _, bad := range []string{"<script", "javascript:", "onerror", "data:text/html"} {
			if strings.Contains(out, bad) { t.Errorf("%q rendered %q", src, out) }
		}
	}
}
func TestRenderKeepsMarkdown(t *testing.T) { /* "**b** `c`\n\n- [ ] task\n\nhttps://example.com" has <strong>, <code>, a checkbox list item, and an <a href="https://example.com" rel="nofollow"> */ }

// flash_test.go
func TestFlashRoundTrip(t *testing.T) {}      // Put then Pop on the next request returns it once
func TestMalformedFlashIgnored(t *testing.T) {} // a garbage vq_flash cookie pops nothing and is expired

// handlers_test.go
func TestHandlersNeverWriteDirectly(t *testing.T) {
	// scan internal/web/handlers/*.go (not tests): no file mentions "gen.New(", ".Exec(", "BeginTx(" or "d.DB." except misc.go's Healthz ping
}
```

- [ ] **Step 2:** Run `go test ./internal/markdown ./internal/web/...` — Expected: FAIL.
- [ ] **Step 3:** Implement the Interfaces. In `serve`, after `openDB`: `c := core.New(w, time.Now)`, `owner, err := c.EnsureOwner(ctx)`.
- [ ] **Step 4:** Run `make gen && go test ./...` — Expected: PASS.
- [ ] **Step 5: Commit** — `"Web: core in deps, flash toasts, markdown, form components"`

---

### Task 6: Project pages

**Files:**
- Create: `internal/web/handlers/projects.go`, `projects_test.go`, `internal/web/templates/projects.templ`, `labels.go` (category, priority and event wording)
- Modify: `internal/web/router.go`, `internal/web/handlers/home.go` (`/` becomes the project list), `internal/web/templates/home.templ` (removed or folded into `projects.templ`)

**Interfaces:**
- Routes:

| Method and path | Handler | Does |
|---|---|---|
| `GET /` | `ProjectList` | active projects (key, name, open ticket count), archived ones collapsed under "Archived", a "New project" link; empty state "No projects yet." |
| `GET /projects/new`, `POST /projects` | `ProjectNew`, `ProjectCreate` | form: key, name, description; success → `/p/{key}` with "Project created." |
| `GET /p/{key}` | `ProjectShow` | tickets grouped by status in status order (key, title, priority, assignee, tags); "New ticket"; each group shows its count; an empty status shows "No tickets." |
| `GET /p/{key}/settings` | `ProjectSettings` | name and description form; statuses (rename, up, down, delete, add with category); milestones (create, edit, close, reopen); archive or restore |
| `POST /p/{key}` | `ProjectUpdate` | name, description |
| `POST /p/{key}/archive`, `/restore` | `ProjectArchive`, `ProjectRestore` | |
| `POST /p/{key}/statuses`, `/statuses/{id}/rename`, `/up`, `/down`, `/delete` | status handlers | |
| `POST /p/{key}/milestones`, `/milestones/{id}`, `/close`, `/reopen` | milestone handlers | |

- `{key}` is matched case-insensitively; a non-canonical key redirects 301 to the canonical path. An unknown key redirects to `/` with "No project VAQ." (key as typed, uppercased).
- `labels.go`: `CategoryLabel(c string) string` ("Backlog", "Todo", "In progress", "In review", "Done", "Cancelled"), `PriorityLabel(p int64) string` ("No priority", "Urgent", "High", "Medium", "Low"), and the priority `<select>` options in that order.

- [ ] **Step 1: Write the failing tests** in `projects_test.go` using `webtest`: `TestEmptyHome` ("No projects yet."), `TestCreateProjectFlow` (POST then GET `/p/VAQ` shows six status groups), `TestCreateProjectInvalidKeepsInput` (422, message under the key field, typed name still in the form), `TestProjectKeyCaseRedirect` (`/p/vaq` → 301 `/p/VAQ`), `TestUnknownProjectRedirects`, `TestStatusSettings` (add, rename, move, delete-refused-with-flash), `TestMilestoneSettings`, `TestArchiveHidesFromList`.
- [ ] **Step 2:** Run `go test ./internal/web/...` — Expected: FAIL.
- [ ] **Step 3:** Implement handlers, templates and routes. Every POST ends in a 303 or a 422 re-render.
- [ ] **Step 4:** Run `make gen && go test ./internal/web/...` — Expected: PASS.
- [ ] **Step 5: Commit** — `"Web: project list, project page and settings"`

---

### Task 7: Ticket pages, sub-tickets, comments and links

**Files:**
- Create: `internal/web/handlers/tickets.go`, `tickets_test.go`, `internal/web/templates/tickets.templ`
- Modify: `internal/web/router.go`

**Interfaces:**
- Routes:

| Method and path | Handler | Does |
|---|---|---|
| `GET /p/{key}/new` (optional `?parent=VAQ-3`), `POST /p/{key}/tickets` | `TicketNew`, `TicketCreate` | form: title, body (markdown), status, priority, assignee (None or each actor), parent (ticket key text, prefilled from `?parent`), milestone (open ones), tags (comma-separated); success → the ticket with "Ticket VAQ-4 created." |
| `GET /t/{ticket}` | `TicketShow` | key and title; rendered body; a side panel with status, priority, assignee, milestone, parent (link), tags; sub-tickets with their status and "New sub-ticket"; links grouped "Blocks", "Blocked by", "Relates to", "Duplicates", "Duplicated by", each with Remove, and an add-link form (kind + ticket key); comments oldest first with Edit/Delete on your own; a comment form; the ticket's activity (from `ListTicketEvents`) last |
| `GET /t/{ticket}/edit`, `POST /t/{ticket}` | `TicketEdit`, `TicketUpdate` | the same form, filled |
| `POST /t/{ticket}/comments`, `/comments/{id}`, `/comments/{id}/delete` | comment handlers | |
| `POST /t/{ticket}/links`, `/links/{id}/delete` | link handlers | |

- `{ticket}` goes through `core.ParseTicketKey`; a parseable but non-canonical key redirects 301 to the canonical path (`/t/vaq-012` → `/t/VAQ-12`); an unparseable or unknown key redirects to `/` with "No ticket VAQ-99." or "That isn't a ticket key."
- Parent and link targets are typed as ticket keys and resolved with `ResolveTicket`; an unknown key is a field error, not a 500.
- The comment edit form appears in place on `?edit={commentID}`; no JavaScript needed.

- [ ] **Step 1: Write the failing tests:** `TestCreateTicketFlow`, `TestCreateTicketInvalid` (empty title → 422 with "Enter a title." and other inputs kept), `TestSubTicketFromParent` (`?parent=VAQ-1` prefills, the parent page lists it), `TestTicketKeyCanonicalRedirect` and `TestUnknownTicketRedirects` (Review Focus 4: `vaq-1`, `VAQ-001`, `VAQ-99999`, `nonsense`), `TestParentCycleShownAsFieldError`, `TestCommentLifecycle` (add, edit via `?edit=`, delete), `TestLinkLifecycle` (add by lowercase key, shown under "Blocks" on one ticket and "Blocked by" on the other, remove; unknown key → field error), `TestTicketBodyRenderedSafely` (a `<script>` body appears escaped on the page).
- [ ] **Step 2:** Run `go test ./internal/web/...` — Expected: FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run `make gen && go test ./internal/web/...` — Expected: PASS.
- [ ] **Step 5: Commit** — `"Web: ticket pages, sub-tickets, comments, links"`

---

### Task 8: Activity feed, docs and wrap-up

**Files:**
- Create: `internal/web/handlers/activity.go`, `activity_test.go`; add `ActivityPage` and the shared `EventLine` component to `internal/web/templates/activity.templ`
- Modify: `internal/web/router.go`, `internal/web/templates/layout.templ` (top bar: "Projects", "Activity"), `CLAUDE.md`, `.claude/rules/handlers.md`, `.claude/rules/database.md`, `docs/BACKLOG.md`, `coverage-floors.txt`

**Interfaces:**
- `GET /activity`: the newest 100 events across the instance, each as one line: actor name, a plain sentence from `EventLine` (for example "Owner created VAQ-4 Fix login", "Owner moved VAQ-4 from Todo to In progress", "Owner commented on VAQ-4"), the project key, and a relative time through `timefmt`. The ticket page's activity section (Task 7) uses the same `EventLine`.
- `EventLine` covers every `Ev*` kind in core; an unknown kind renders "Owner made a change" rather than failing.

- [ ] **Step 1: Write the failing tests:** `TestActivityShowsRecentEvents` (create a project and a ticket, move it, comment; the page lists four lines newest first), `TestEveryEventKindHasWording` (iterate over all `core.Ev*` constants through an exported `core.EventKinds()` slice; each renders a non-fallback sentence).
- [ ] **Step 2:** Run `go test ./internal/web/...` — Expected: FAIL.
- [ ] **Step 3:** Implement. Add `func EventKinds() []string` to `internal/core/events.go`.
- [ ] **Step 4:** Update docs:
  - `CLAUDE.md` package map: `internal/core`, `internal/markdown`, `internal/web/flash`, the new handlers, templates and `Deps` shape.
  - `.claude/rules/handlers.md`: "Writes go through internal/core" is now in force, and `TestHandlersNeverWriteDirectly` holds it.
  - `.claude/rules/database.md`: fix the stale migration-order line (Phase 0 backlog item); note that only `internal/core` writes tracker tables.
  - `docs/BACKLOG.md`: tick Phase 1a; tick the `database.md` item; add anything deferred during this plan.
  - `coverage-floors.txt`: add `internal/core`, `internal/markdown` and `internal/web/flash` at 5 points below measured, rounded down to a multiple of 5.
- [ ] **Step 5:** Run `make ci` — Expected: PASS.
- [ ] **Step 6:** Manual check with `make dev` (port 7879): create a project, three tickets (one a sub-ticket), tag, link, comment, edit, rename a status; read `/activity`. Expected: every page renders with the theme, every action shows a toast, no console errors.
- [ ] **Step 7: Commit** — `"Web: activity feed; docs for phase 1a"`
