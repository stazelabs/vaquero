package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/gitread"
	"github.com/stazelabs/vaquero/internal/web/templates"
)

// approvedPin is the pin of path in the latest answered plan approval of
// the ticket that reference refID belongs to, when that ticket is in p,
// the reference names path, and the approval approved; nil otherwise.
func approvedPin(ctx context.Context, d Deps, p gen.Project, path, refID string) (*core.FilePin, error) {
	if refID == "" {
		return nil, nil
	}
	ref, err := d.R.GetTicketRef(ctx, refID)
	if err != nil {
		return nil, nil // no such reference: nothing to compare
	}
	t, err := d.R.GetTicket(ctx, ref.TicketID)
	if err != nil || t.ProjectID != p.ID || ref.Target != path {
		return nil, nil
	}
	rows, err := d.R.ListPlanApprovalAnswers(ctx, []string{t.ID})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 || !rows[0].Approved { // latest answer first
		return nil, nil
	}
	var pins core.ApprovalPins
	if json.Unmarshal([]byte(rows[0].Pins), &pins) != nil {
		return nil, nil
	}
	for _, f := range pins.Files {
		if f.Path == path {
			return &f, nil
		}
	}
	return nil, nil
}

// pinState is pin against main in repo: its state, and main's head.
func pinState(ctx context.Context, repo *gitread.Repo, pin core.FilePin) (core.FileState, string, error) {
	head, err := repo.Head(ctx)
	if err != nil {
		return core.FileState{}, "", err
	}
	got, err := core.CheckFilePins(ctx, repo, head, []core.FilePin{pin})
	if err != nil {
		return core.FileState{}, "", err
	}
	return got[0], head, nil
}

// fileSince is the file view's note for a file opened from an approved
// ticket's reference (?ref=ID): changed, renamed or gone since the
// approval pinned it, or that it can't be checked (a read that fails, or
// a pin taken against other repo settings); nil when it is as approved,
// or there is no such approval.
func fileSince(ctx context.Context, d Deps, repo *gitread.Repo, p gen.Project, path, refID string) *templates.FileSince {
	pin, err := approvedPin(ctx, d, p, path, refID)
	if err != nil {
		d.Logger.ErrorContext(ctx, "file since approval", "err", err)
		return nil
	}
	if pin == nil {
		return nil
	}
	unchecked := &templates.FileSince{State: core.FileUnchecked}
	if !core.PinMatches(*pin, p) {
		return unchecked // pinned against other repo settings
	}
	st, _, err := pinState(ctx, repo, *pin)
	if err != nil {
		if ctx.Err() == nil {
			d.Logger.WarnContext(ctx, "file since approval", "project", p.Key, "err", err)
		}
		return unchecked
	}
	s := &templates.FileSince{State: st.State, NewPath: st.Now}
	switch st.State {
	case core.FileChanged:
		u := url.URL{Path: "/p/" + p.Key + "/file-changes/" + path, RawQuery: url.Values{"ref": {refID}}.Encode()}
		s.DiffURL = u.String()
	case core.FileRenamed, core.FileGone, core.FileUnchecked:
	default:
		return nil
	}
	return s
}

// FileChanges is GET /p/{key}/file-changes/*?ref=ID: the line diff of a
// referenced file between the version its ticket's approval pinned and
// main, read into the file view's "Show changes" when that is first
// opened.
func FileChanges(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := projectFromURL(w, r, d)
		if !ok {
			return
		}
		name := filePath(r)
		if !gitread.ValidPath(name) {
			NotFound(d)(w, r)
			return
		}
		ctx := r.Context()
		show := func(lines []templates.DiffLine, note string) {
			renderPart(w, r, d, http.StatusOK, templates.FileChanges(lines, note))
		}
		repo, f := openRepo(ctx, d, p)
		if f != nil {
			show(nil, f.Message)
			return
		}
		pin, err := approvedPin(ctx, d, p, name, r.URL.Query().Get("ref"))
		if err != nil {
			serverError(w, r, d, err)
			return
		}
		if pin == nil {
			show(nil, "This file has no approved version to compare with.")
			return
		}
		if !core.PinMatches(*pin, p) {
			show(nil, "Can't check this file against the approval.")
			return
		}
		st, head, err := pinState(ctx, repo, *pin)
		if err == nil && st.State == core.FileUnchecked {
			show(nil, "Can't check this file against the approval.")
			return
		}
		if err == nil && st.State != core.FileChanged {
			show(nil, "No changes since approval.")
			return
		}
		var before, after gitread.File
		if err == nil {
			now := name
			if st.Now != "" {
				now = st.Now
			}
			if before, err = repo.Show(ctx, pin.Commit, pin.Path); err == nil {
				after, err = repo.Show(ctx, head, now)
			}
		}
		switch {
		case err == nil:
		case errors.Is(err, gitread.ErrNoFile), errors.Is(err, gitread.ErrNoRev), repoTrouble(ctx, d, p, err) != nil:
			show(nil, "Can't read the changes now.")
			return
		default:
			serverError(w, r, d, err)
			return
		}
		if before.TooLarge || after.TooLarge {
			show(nil, "The file is too large to show its changes.")
			return
		}
		if !textual(before.Text) || !textual(after.Text) {
			show(nil, "This file isn't text, so its changes aren't shown.")
			return
		}
		show(templates.LineDiff(before.Text, after.Text), "")
	}
}

// textual reports whether s is shown as text: valid UTF-8 with no NUL.
func textual(s string) bool { return utf8.ValidString(s) && !strings.ContainsRune(s, 0) }
