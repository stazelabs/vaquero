package api

// The API's objects. Tickets are named by key ("VAQ-12"); internal ids
// appear only where an object has no key (comments and links), so they can
// be edited or removed.

type Project struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Archived    bool   `json:"archived"`
	OpenCount   int64  `json:"open_count"`
}

type Status struct {
	Name     string `json:"name"`
	Category string `json:"category"`
	Position int64  `json:"position"`
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
}

type Ticket struct {
	TicketSummary
	Body       string          `json:"body"`
	Parent     string          `json:"parent,omitempty"`
	Milestone  string          `json:"milestone,omitempty"`
	SubTickets []TicketSummary `json:"sub_tickets"`
	Links      []Link          `json:"links"`
	Comments   []Comment       `json:"comments"`
	CreatedAt  string          `json:"created_at"`
	UpdatedAt  string          `json:"updated_at"`
}

type Link struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`      // blocks, relates, duplicates
	Direction string `json:"direction"` // out: this ticket → key; in: key → this ticket
	Key       string `json:"key"`
	Title     string `json:"title"`
}

type Comment struct {
	ID        string `json:"id"`
	Author    string `json:"author"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
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
