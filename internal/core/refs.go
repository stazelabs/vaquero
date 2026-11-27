package core

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/gitread"
	"github.com/stazelabs/vaquero/internal/idgen"
	"github.com/stazelabs/vaquero/internal/timefmt"
)

// RefKinds are what a reference can be of, in the order they are offered.
var RefKinds = []string{"requirement", "design", "plan", "spec", "other"}

// Limits on a reference's fields.
const (
	MaxRefLabel  = 120
	MaxRefTarget = 2000
)

var (
	refAnchor = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,79}$`)
	// refScheme is the start of something written as a URL (or any other
	// scheme: javascript:, data:, file:), which a repo path never is.
	refScheme = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`)
)

// RefInput is a reference to add: Kind is one of RefKinds; Target a repo
// path (relative to the project's repository) or an http or https URL;
// Anchor an optional heading anchor, for the link only (pins are per file);
// Label an optional name shown instead of the target.
type RefInput struct {
	Kind, Target, Anchor, Label string
}

// AddRef adds a reference to a ticket (VAQ-136). A repo path needs the
// project's repository (RepoPath) and must pass gitread.ValidPath; it need
// not exist on main, and adding never runs git. A ticket references a
// target and anchor once.
func (c *Core) AddRef(ctx context.Context, by, ticketID string, in RefInput) (gen.TicketRef, error) {
	var ref gen.TicketRef
	err := c.tx(ctx, func(q *gen.Queries) error {
		t, err := q.GetTicket(ctx, ticketID)
		if err != nil {
			return notFound(err, "get ticket")
		}
		p, err := q.GetProject(ctx, t.ProjectID)
		if err != nil {
			return fmt.Errorf("get project: %w", err)
		}
		f := fields{}
		kind := strings.ToLower(strings.TrimSpace(in.Kind))
		if !slices.Contains(RefKinds, kind) {
			f.add("kind", "Choose requirement, design, plan, spec or other.")
		}
		target := refTarget(f, strings.TrimSpace(in.Target), p)
		anchor := strings.TrimSpace(in.Anchor)
		if anchor != "" && !refAnchor.MatchString(anchor) {
			f.add("anchor", "Use a heading anchor: lower-case letters, digits and hyphens.")
		}
		label := checked(f, "label", strings.TrimSpace(norm.NFC.String(in.Label)), MaxRefLabel, "", "Keep the label under 120 characters.")
		if err := f.err(); err != nil {
			return err
		}
		key := TicketKey{Project: p.Key, Number: t.Number}.String()
		n, err := q.CountTicketRef(ctx, gen.CountTicketRefParams{TicketID: t.ID, Target: target, Anchor: anchor})
		if err != nil {
			return fmt.Errorf("check reference: %w", err)
		}
		if n > 0 {
			return invalid("target", key+" already references "+target+".")
		}
		ref, err = q.CreateTicketRef(ctx, gen.CreateTicketRefParams{ID: idgen.New(), TicketID: t.ID, Kind: kind, Target: target,
			Anchor: anchor, Label: label, CreatedBy: by, CreatedAt: timefmt.Format(c.Now())})
		if err != nil {
			return fmt.Errorf("create reference: %w", err)
		}
		return c.emit(ctx, q, by, EvRefAdded, t.ProjectID, t.ID, RefPayload{RefID: ref.ID, Kind: kind, Target: target, Anchor: anchor})
	})
	return ref, err
}

// refTarget checks a reference's target and returns it as stored.
func refTarget(f fields, target string, p gen.Project) string {
	switch {
	case target == "":
		f.add("target", "Enter a repo path or a URL.")
	case hasControl(target):
		f.add("target", controlMsg)
	case utf8.RuneCountInString(target) > MaxRefTarget:
		f.add("target", "Keep the target under 2,000 characters.")
	case refScheme.MatchString(target):
		u, err := url.Parse(target)
		switch {
		case err != nil:
			f.add("target", "That isn't a URL.")
		case !strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https"):
			f.add("target", "Use a repo path or an http or https URL.")
		case u.Host == "" || u.Hostname() == "":
			f.add("target", "That isn't a URL.")
		case u.User != nil:
			f.add("target", "Leave the user name and password out of the URL.")
		}
	case !gitread.ValidPath(target):
		f.add("target", "That isn't a repo path.")
	case p.RepoPath == "":
		f.add("target", "Set the project's repository first.")
	}
	return target
}

// RemoveRef deletes ticketID's reference refID. A reference of another
// ticket is ErrNotFound, as an id that doesn't exist is (VAQ-167).
func (c *Core) RemoveRef(ctx context.Context, by, ticketID, refID string) error {
	return c.tx(ctx, func(q *gen.Queries) error {
		ref, err := q.GetTicketRef(ctx, refID)
		if err != nil {
			return notFound(err, "get reference")
		}
		if ref.TicketID != ticketID {
			return ErrNotFound
		}
		t, err := q.GetTicket(ctx, ref.TicketID)
		if err != nil {
			return fmt.Errorf("get ticket: %w", err)
		}
		if err := q.DeleteTicketRef(ctx, ref.ID); err != nil {
			return fmt.Errorf("delete reference: %w", err)
		}
		return c.emit(ctx, q, by, EvRefRemoved, t.ProjectID, t.ID, RefPayload{RefID: ref.ID, Kind: ref.Kind, Target: ref.Target, Anchor: ref.Anchor})
	})
}
