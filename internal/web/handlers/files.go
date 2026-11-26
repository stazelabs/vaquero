package handlers

import (
	"context"
	"net/http"

	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/markdown"
	"github.com/stazelabs/vaquero/internal/timefmt"
	"github.com/stazelabs/vaquero/internal/units"
	"github.com/stazelabs/vaquero/internal/web/templates"
)

// FileView is GET /p/{key}/files/*?at=REV: a repo file at main, or at a
// revision. Opened at main from a ticket's reference (?ref=ID) whose
// approval pinned the file, it says when the file changed, was renamed or
// left main since (fileSince). A path or revision Vaquero won't pass to git is the 404 page,
// before git runs; a missing file or revision is a 404 that names it.
func FileView(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := projectFromURL(w, r, d)
		if !ok {
			return
		}
		name, at, f := fileInput(r)
		if f != nil {
			NotFound(d)(w, r)
			return
		}
		v := templates.FileView{Project: p, Path: name, At: at, Main: p.MainBranch, MainURL: fileURL(p.Key, name, "")}
		repo, f := openRepo(r.Context(), d, p)
		if f != nil {
			v.Problem = f.Message
			render(w, r, d, f.Status, templates.FilePage(v))
			return
		}
		v.Main = repo.Main
		if at == "" {
			v.Since = fileSince(r.Context(), d, repo, p, name, r.URL.Query().Get("ref"))
		}
		file, f, err := readFile(r.Context(), repo, name, at)
		if err != nil {
			f = repoTrouble(r.Context(), d, p, err)
		}
		if err != nil && f == nil {
			serverError(w, r, d, err)
			return
		}
		if f != nil {
			v.Problem = f.Message
			render(w, r, d, f.Status, templates.FilePage(v))
			return
		}
		v.Size, v.TooLarge, v.Binary, v.Markdown = units.SizeWords(file.Size), file.TooLarge, file.Binary, file.Markdown
		if file.Text != nil {
			if file.Markdown {
				v.Body = markdown.RenderDoc(*file.Text)
			} else {
				v.Text = *file.Text
			}
		}
		render(w, r, d, http.StatusOK, templates.FilePage(v))
	}
}

// commitsView is the ticket page's Commits section before its fragment
// loads: the problem at once when there is no repository or no git (no git
// runs), else a section that asks for the fragment.
func commitsView(d Deps, p gen.Project, key string) templates.CommitsView {
	v := templates.CommitsView{Key: key, ProjectKey: p.Key, URL: "/t/" + key + "/commits"}
	switch {
	case p.RepoPath == "":
		v.Problem = "No repository set for this project."
	case d.Git == nil || !d.Git.Available():
		v.Problem = "Git isn't installed."
	default:
		v.Lazy = true
	}
	return v
}

// TicketCommits is GET /t/{ticket}/commits, the ticket page's Commits
// section, read once the page has loaded so git never slows the page.
func TicketCommits(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, p, ok := ticketFromURL(w, r, d)
		if !ok {
			return
		}
		v, err := loadCommits(r.Context(), d, t, p)
		switch {
		case err == nil:
		case r.Context().Err() != nil:
			return // the tab closed; nothing to answer or log
		default:
			if f := repoTrouble(r.Context(), d, p, err); f != nil {
				v.Problem = f.Message
				break
			}
			d.Logger.ErrorContext(r.Context(), "ticket commits", "err", err)
			v.Problem = "Can't read the commits now."
		}
		renderPart(w, r, d, http.StatusOK, templates.CommitsSection(v))
	}
}

func loadCommits(ctx context.Context, d Deps, t gen.Ticket, p gen.Project) (templates.CommitsView, error) {
	key := ticketKey(p, t.Number)
	v := commitsView(d, p, key)
	v.Lazy = false
	if v.Problem != "" {
		return v, nil
	}
	repo, f := openRepo(ctx, d, p)
	if f != nil {
		v.Problem = f.Message
		return v, nil
	}
	tc, err := ticketCommits(ctx, d, repo, t, p)
	if err != nil {
		return v, err
	}
	now := d.Now()
	for _, c := range tc.Commits {
		at, _ := timefmt.Parse(c.At)
		v.Commits = append(v.Commits, templates.CommitRow{Short: c.Short, Subject: c.Subject, Author: c.Author, Ago: timefmt.Ago(at, now, nil)})
	}
	v.Added, v.Removed, v.Released = tc.Diffstat.Added, tc.Diffstat.Removed, tc.Released
	for _, f := range tc.Diffstat.Files {
		v.Files = append(v.Files, templates.CommitFile{Path: f, URL: fileURL(p.Key, f, "")})
	}
	return v, nil
}
