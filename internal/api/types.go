package api

import "strings"

// The API's objects. Tickets are named by key ("VAQ-12"); internal ids
// appear only where an object has no key (comments and links), so they can
// be edited or removed.

type Project struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Archived    bool   `json:"archived"`
	OpenCount   int64  `json:"open_count"`
	// Workflow is the agent workflow switch. TierMap is the project's
	// effective tier map, harness → tier → model: its own, else the
	// default.
	Workflow bool                         `json:"workflow"`
	TierMap  map[string]map[string]string `json:"tier_map"`
	// Icon is how the project is drawn: its monogram, a glyph, or an
	// uploaded image served at URL.
	Icon *ProjectIcon `json:"icon,omitempty"`
}

// ProjectIcon is a project's icon. Kind is monogram, glyph or image; Glyph
// names the glyph (kind glyph only) and URL is the image's address (kind
// image only).
type ProjectIcon struct {
	Kind  string `json:"kind"`
	Glyph string `json:"glyph,omitempty"`
	URL   string `json:"url,omitempty"`
}

type Status struct {
	Name     string `json:"name"`
	Category string `json:"category"`
	Position int64  `json:"position"`
	// TicketCount is how many tickets have this status.
	TicketCount int64 `json:"ticket_count"`
}

// Tag is one of a project's tags: a tag is shared by every project, so
// TicketCount counts only this project's tickets that carry it.
type Tag struct {
	Name        string `json:"name"`
	TicketCount int64  `json:"ticket_count"`
}

type Milestone struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	DueOn       string `json:"due_on,omitempty"`
	Closed      bool   `json:"closed"`
	Done        int64  `json:"done"`
	Total       int64  `json:"total"`
}

type TicketSummary struct {
	Key      string   `json:"key"`
	Title    string   `json:"title"`
	Status   string   `json:"status"`
	Category string   `json:"category"`
	Priority int64    `json:"priority"`
	Assignee string   `json:"assignee,omitempty"`
	Tags     []string `json:"tags"`
	Ready    bool     `json:"ready"`
	Version  int64    `json:"version"`
	// ClaimedBy and ClaimExpiresAt are set while a claim is held.
	ClaimedBy      string `json:"claimed_by,omitempty"`
	ClaimExpiresAt string `json:"claim_expires_at,omitempty"`
	// Tier is the execution tier, absent when unset. On a summary it is
	// given only when the project's agent workflow is on; a full Ticket
	// gives it always (Ticket.Tier).
	Tier string `json:"tier,omitempty"`
	// Readiness is the agent workflow's gate, given only when the
	// project's workflow is on: on every full ticket, and on the summary
	// of a todo ticket. Ready above is then the board's rule and this one.
	Readiness *Readiness `json:"readiness,omitempty"`
}

// Readiness says whether a ticket passes the agent workflow's gate: a
// todo, with a tier, and a current plan approval. Reasons lists what
// fails, in a fixed order; it is empty when Ready.
type Readiness struct {
	Ready   bool     `json:"ready"`
	Reasons []string `json:"reasons"`
}

type Ticket struct {
	TicketSummary
	Body      string `json:"body"`
	Parent    string `json:"parent,omitempty"`
	Milestone string `json:"milestone,omitempty"`
	// Tier is the execution tier (design, light, standard or heavy) and
	// Model an exact model for it; both absent when unset. Tier here
	// shadows TicketSummary.Tier, so a full ticket gives it always.
	Tier        string          `json:"tier,omitempty"`
	Model       string          `json:"model,omitempty"`
	SubTickets  []TicketSummary `json:"sub_tickets"`
	Links       []Link          `json:"links"`
	Comments    []Comment       `json:"comments"`
	Attachments []Attachment    `json:"attachments"`
	// FormerKeys are the keys the ticket had before it moved to another
	// project, each still leading to it; absent when it never moved.
	FormerKeys []string `json:"former_keys,omitempty"`
	CreatedAt  string   `json:"created_at"`
	UpdatedAt  string   `json:"updated_at"`
	// Warning is set on a write's answer when someone other than the
	// caller holds a claim on the ticket: "Claimed by bot until 12:15
	// UTC." Claims are advisory, so the write went through.
	Warning string `json:"warning,omitempty"`
}

// TicketBrief is the short answer to a write (?view=summary): the
// ticket's state after it, and for a new comment the comment's id.
type TicketBrief struct {
	ID             string `json:"id,omitempty"` // the new comment's, for a comment
	Key            string `json:"key"`
	Status         string `json:"status"`
	Category       string `json:"category"`
	Version        int64  `json:"version"`
	Assignee       string `json:"assignee,omitempty"`
	ClaimedBy      string `json:"claimed_by,omitempty"`
	ClaimExpiresAt string `json:"claim_expires_at,omitempty"`
	Warning        string `json:"warning,omitempty"` // as on Ticket
}

// BulkResult is one item's outcome in POST /api/v1/tickets/bulk: the
// ticket's status and version as the item left it (absent when the key
// names no ticket), and on a failure the refusal and the actions it
// skipped.
type BulkResult struct {
	Key     string   `json:"key"`
	OK      bool     `json:"ok"`
	Status  string   `json:"status,omitempty"`
	Version int64    `json:"version,omitempty"`
	Error   *Failure `json:"error,omitempty"`
	Skipped []string `json:"skipped,omitempty"`
	// Warning is set when someone other than the caller holds a claim on
	// the ticket (claims are advisory; the item still ran).
	Warning string `json:"warning,omitempty"`
}

// BulkAnswer is POST /api/v1/tickets/bulk's answer, one result per item
// in request order.
type BulkAnswer struct {
	Items []BulkResult `json:"items"`
}

type Link struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`      // blocks, relates, duplicates
	Direction string `json:"direction"` // out: this ticket → key; in: key → this ticket
	Key       string `json:"key"`
	Title     string `json:"title"`
	Warning   string `json:"warning,omitempty"` // on a new link's answer, as on Ticket
}

type Comment struct {
	ID        string `json:"id"`
	Author    string `json:"author"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	// On a comment write's answer only: the claim on its ticket, and the
	// warning as on Ticket.
	ClaimedBy      string `json:"claimed_by,omitempty"`
	ClaimExpiresAt string `json:"claim_expires_at,omitempty"`
	Warning        string `json:"warning,omitempty"`
}

// Plan is a ticket's plan at one revision: the current one from GET and
// PUT /tickets/{ticket}/plan, an earlier one from .../plan/revisions/{n}.
// UpdatedBy and UpdatedAt are who wrote that revision, and when.
type Plan struct {
	Ticket    string `json:"ticket"`
	Revision  int64  `json:"revision"`
	Body      string `json:"body"`
	UpdatedBy string `json:"updated_by"`
	UpdatedAt string `json:"updated_at"`
}

// Decision is a question for the owner, a ruling an agent (or the owner)
// made, or an approval of a plan, a merge or a release. Tickets are keys;
// people are names. Answered is "direct" when the owner answered without an
// agent, "relayed" when RelayedBy recorded the owner's answer, quoting
// Words. Approves is an approval's approving option, its Approve label as
// spelled. Approvals are the pins a plan approval recorded, one per ticket.
type Decision struct {
	ID             string             `json:"id"`
	Project        string             `json:"project"`
	Tickets        []string           `json:"tickets"`
	Run            string             `json:"run,omitempty"`
	Kind           string             `json:"kind"`
	Subject        string             `json:"subject,omitempty"`
	Question       string             `json:"question"`
	Options        []DecisionOption   `json:"options"`
	Approves       string             `json:"approves,omitempty"`
	Recommendation string             `json:"recommendation,omitempty"`
	Why            string             `json:"why,omitempty"`
	State          string             `json:"state"`
	Answer         string             `json:"answer,omitempty"`
	Note           string             `json:"note,omitempty"`
	AnsweredBy     string             `json:"answered_by,omitempty"`
	AnsweredAt     string             `json:"answered_at,omitempty"`
	Answered       string             `json:"answered,omitempty"`
	RelayedBy      string             `json:"relayed_by,omitempty"`
	Words          string             `json:"words,omitempty"`
	AskedBy        string             `json:"asked_by"`
	AskedAt        string             `json:"asked_at"`
	Approvals      []DecisionApproval `json:"approvals,omitempty"`
	// Asked is, for a plan approval, what each ticket was asked about: its
	// plan revision, its epic and its sub-tickets then. An Approve covers
	// exactly these, and is refused if any changed since.
	Asked []DecisionAsked `json:"asked,omitempty"`
}

// DecisionAsked is one plan-approval ticket as asked: Parent is its epic's
// key (absent for none), Children its sub-tickets, by key and title. A
// ticket deleted since is left out.
type DecisionAsked struct {
	Ticket       string         `json:"ticket"`
	PlanRevision int64          `json:"plan_revision"`
	Parent       string         `json:"parent,omitempty"`
	Children     []DecisionItem `json:"children"`
}

// DecisionItem is a ticket a decision names, by key and title.
type DecisionItem struct {
	Key   string `json:"key"`
	Title string `json:"title"`
}

// DecisionOption is one answer a decision offers, and what it costs.
type DecisionOption struct {
	Label    string `json:"label"`
	Tradeoff string `json:"tradeoff,omitempty"`
}

// DecisionApproval is one ticket's approval: its requirement's hash and the
// plan revision approved (0: none), as they stood when approved.
type DecisionApproval struct {
	Ticket          string `json:"ticket"`
	RequirementHash string `json:"requirement_hash"`
	PlanRevision    int64  `json:"plan_revision"`
	At              string `json:"at"`
}

// Attachment is a file on a ticket. URL is the address of its bytes on the
// server, as a ticket body or comment refers to it.
type Attachment struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	UploadedBy  string `json:"uploaded_by"`
	CreatedAt   string `json:"created_at"`
	URL         string `json:"url"`
	Warning     string `json:"warning,omitempty"` // on an upload's answer, as on Ticket
}

// inlineTypes are the sniffed types a server shows in the page; every
// other file is a download.
var inlineTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}

// IsInlineType reports whether a stored content type is served inline
// (PNG, JPEG, GIF, WebP) rather than as a download.
func IsInlineType(contentType string) bool {
	t, _, _ := strings.Cut(contentType, ";")
	return inlineTypes[strings.TrimSpace(t)]
}

// Markdown is the reference a ticket body or comment uses for the file: an
// image for an inline type, otherwise a link.
func (a Attachment) Markdown() string {
	label := strings.NewReplacer(`\`, `\\`, `[`, `\[`, `]`, `\]`).Replace(a.Name)
	ref := "[" + label + "](" + a.URL + ")"
	if IsInlineType(a.ContentType) {
		return "!" + ref
	}
	return ref
}

type Event struct {
	Seq     int64          `json:"seq"`
	At      string         `json:"at"`
	Actor   string         `json:"actor"`
	Kind    string         `json:"kind"`
	Project string         `json:"project,omitempty"`
	Ticket  string         `json:"ticket,omitempty"`
	Text    string         `json:"text"` // the activity page's wording
	Payload map[string]any `json:"payload"`
	// Meta is where the change came from, as reported (surface, client,
	// harness, model, run); left out for an event with none.
	Meta map[string]string `json:"meta,omitempty"`
}

type SearchHit struct {
	Key     string `json:"key"`
	Title   string `json:"title"`
	Status  string `json:"status"`
	Snippet string `json:"snippet"` // [ and ] around the matched words
}

type Token struct {
	Name       string `json:"name"`
	Prefix     string `json:"prefix"`
	Actor      string `json:"actor"`
	CreatedAt  string `json:"created_at"`
	LastUsedAt string `json:"last_used_at,omitempty"`
	Revoked    bool   `json:"revoked"`
	Token      string `json:"token,omitempty"` // only in the answer that creates it
}
