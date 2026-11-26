package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/gitread"
	"github.com/stazelabs/vaquero/internal/idgen"
	"github.com/stazelabs/vaquero/internal/timefmt"
)

// ProjectInput is a new project. Key is immutable once created.
type ProjectInput struct{ Key, Name, Description string }

var projectKeyRe = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,9}$`)

// defaultStatuses are seeded into every new project, in order.
var defaultStatuses = [][2]string{
	{"Backlog", "backlog"}, {"Todo", "todo"}, {"In progress", "in_progress"},
	{"In review", "in_review"}, {"Done", "done"}, {"Cancelled", "cancelled"},
}

// CreateProject makes a project and seeds its default statuses.
func (c *Core) CreateProject(ctx context.Context, by string, in ProjectInput) (gen.Project, error) {
	f := fields{}
	key := strings.TrimSpace(in.Key)
	if !projectKeyRe.MatchString(key) {
		f.add("key", "Use 2 to 10 capital letters or digits, starting with a letter.")
	}
	name := text(f, "name", in.Name, 120, "Enter a name.", "Keep the name under 120 characters.")
	desc := text(f, "description", in.Description, 20000, "", "Keep the description under 20,000 characters.")
	var p gen.Project
	err := c.tx(ctx, func(q *gen.Queries) error {
		if _, ok := f["key"]; !ok {
			if _, err := q.GetProjectByKey(ctx, key); err == nil {
				f.add("key", "That key is taken.")
			} else if !errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("check key: %w", err)
			}
		}
		if err := f.err(); err != nil {
			return err
		}
		now := timefmt.Format(c.Now())
		var err error
		p, err = q.CreateProject(ctx, gen.CreateProjectParams{ID: idgen.New(), Key: key, Name: name, Description: desc, CreatedAt: now, UpdatedAt: now})
		if err != nil {
			return fmt.Errorf("create project: %w", err)
		}
		for i, s := range defaultStatuses {
			if _, err := q.CreateStatus(ctx, gen.CreateStatusParams{ID: idgen.New(), ProjectID: p.ID, Name: s[0], Category: s[1], Position: int64(i + 1)}); err != nil {
				return fmt.Errorf("seed status: %w", err)
			}
		}
		return c.emit(ctx, q, by, EvProjectCreated, p.ID, "", map[string]string{"key": key, "name": name})
	})
	return p, err
}

// ProjectPatch names a project's fields to change; a nil field keeps its
// value. TierMap is JSON (ParseTierMap), "" for the default map. IconKind
// is monogram, glyph or image (the stored image; SetProjectIcon uploads
// one), with IconGlyph naming the glyph; a nil IconGlyph is "".
type ProjectPatch struct {
	Name, Description *string
	Workflow          *bool
	TierMap           *string
	IconKind          *string
	IconGlyph         *string
	// RepoPath, MainBranch and Remote are the project's repository
	// (VAQ-136); only the owner changes them (ruling P2-R1), and a changed
	// set is checked against git before the transaction. FetchMinutes is
	// open to anyone.
	RepoPath, MainBranch, Remote *string
	FetchMinutes                 *int64
}

// MinFetchMinutes and MaxFetchMinutes bound a project's fetch interval;
// 0 means never.
const (
	MinFetchMinutes = 5
	MaxFetchMinutes = 1440
)

// repoSettings is the three settings that decide where a project's repo is.
type repoSettings struct{ path, main, remote string }

func (p ProjectPatch) repo(cur gen.Project) repoSettings {
	r := repoSettings{cur.RepoPath, cur.MainBranch, cur.Remote}
	if p.RepoPath != nil {
		r.path = strings.TrimSpace(*p.RepoPath)
	}
	if p.MainBranch != nil {
		r.main = strings.TrimSpace(*p.MainBranch)
	}
	if p.Remote != nil {
		r.remote = strings.TrimSpace(*p.Remote)
	}
	return r
}

// checkRepo validates a changed repo setting against git, outside any
// transaction. It returns the settings to store (the path as the work tree's
// root) or the refusals by field.
func (c *Core) checkRepo(ctx context.Context, r repoSettings) (repoSettings, fields) {
	f := fields{}
	if hasControl(r.path) || hasControl(r.main) || hasControl(r.remote) {
		f.add("repo_path", controlMsg)
		return r, f
	}
	if r.main == "" {
		f.add("main_branch", "Enter the main branch's name.")
	} else if !gitread.ValidRev(r.main) {
		f.add("main_branch", "That isn't a branch name.")
	}
	if r.path == "" || len(f) > 0 {
		return r, f
	}
	if !filepath.IsAbs(r.path) {
		f.add("repo_path", "Use an absolute path.")
		return r, f
	}
	if c.Git == nil {
		f.add("repo_path", "Git isn't installed, so repository features are off.")
		return r, f
	}
	repo, err := c.Git.Open(ctx, r.path, r.main, r.remote)
	switch {
	case errors.Is(err, gitread.ErrNoGit):
		f.add("repo_path", "Git isn't installed, so repository features are off.")
	case errors.Is(err, gitread.ErrOldGit):
		f.add("repo_path", "Git 2.31 or later is needed, so repository features are off.")
	case errors.Is(err, gitread.ErrPartialClone):
		f.add("repo_path", "Partial clones need git 2.44 or later.")
	case errors.Is(err, gitread.ErrNotRepo):
		f.add("repo_path", "That isn't a git work tree.")
	case errors.Is(err, gitread.ErrInvalid):
		f.add("remote", "That isn't a remote name.")
	case err != nil:
		f.add("repo_path", "Can't read the repo: "+err.Error())
	}
	if len(f) > 0 {
		return r, f
	}
	if _, err := repo.Head(ctx); err != nil {
		f.add("main_branch", "The repo has no branch "+r.main+".")
	}
	if r.remote != "" {
		remotes, err := repo.Remotes(ctx)
		if err != nil {
			f.add("remote", "Can't read the repo: "+err.Error())
		} else if !slices.Contains(remotes, r.remote) {
			f.add("remote", "The repo has no remote "+r.remote+".")
		}
	}
	r.path = repo.Path
	return r, f
}

// UpdateProject changes the fields patch names. The key never changes
// here (ChangeProjectKey). A patch that changes nothing writes nothing.
func (c *Core) UpdateProject(ctx context.Context, by, projectID string, patch ProjectPatch) (gen.Project, error) {
	var p gen.Project
	// Repo settings: the owner check and the git check run before the
	// transaction (git never runs inside it); the transaction checks again
	// that the stored settings are still the ones that were checked.
	var asked, checked repoSettings
	var repoRefusal fields
	if patch.RepoPath != nil || patch.MainBranch != nil || patch.Remote != nil {
		q := gen.New(c.DB)
		cur, err := q.GetProject(ctx, projectID)
		if err != nil {
			return p, notFound(err, "get project")
		}
		asked = patch.repo(cur)
		if want := asked; want != (repoSettings{cur.RepoPath, cur.MainBranch, cur.Remote}) {
			owner, err := byOwner(ctx, q, by)
			if err != nil {
				return p, err
			}
			if !owner {
				return p, invalid(repoField(cur, want), "Only the owner changes the repo settings.")
			}
			checked, repoRefusal = c.checkRepo(ctx, want)
		}
	}
	err := c.tx(ctx, func(q *gen.Queries) error {
		var err error
		if p, err = q.GetProject(ctx, projectID); err != nil {
			return notFound(err, "get project")
		}
		f := fields{}
		name, description, workflow, tierMap := p.Name, p.Description, p.Workflow == 1, p.TierMap
		if patch.Name != nil {
			name = text(f, "name", *patch.Name, 120, "Enter a name.", "Keep the name under 120 characters.")
		}
		if patch.Description != nil {
			description = text(f, "description", *patch.Description, 20000, "", "Keep the description under 20,000 characters.")
		}
		if patch.Workflow != nil {
			workflow = *patch.Workflow
		}
		if patch.TierMap != nil {
			if tierMap, err = storedTierMap(*patch.TierMap); err != nil {
				inv, _ := errors.AsType[*Invalid](err)
				maps.Copy(f, inv.Fields)
			}
		}
		// Ruling N-R6: the workflow switch and the tier map are the
		// owner's alone, acting directly (an agent acting for the owner
		// is its own actor).
		if workflow != (p.Workflow == 1) || (patch.TierMap != nil && tierMap != p.TierMap) {
			owner, err := byOwner(ctx, q, by)
			if err != nil {
				return err
			}
			if !owner && workflow != (p.Workflow == 1) {
				f.add("workflow", "Only the owner turns the agent workflow on or off.")
			}
			if !owner && patch.TierMap != nil && tierMap != p.TierMap {
				f.add("tier_map", "Only the owner changes the tier map.")
			}
		}
		repo := repoSettings{p.RepoPath, p.MainBranch, p.Remote}
		if want := patch.repo(p); want != repo {
			owner, err := byOwner(ctx, q, by)
			if err != nil {
				return err
			}
			switch {
			case !owner:
				f.add(repoField(p, want), "Only the owner changes the repo settings.")
			case want != asked:
				f.add("repo_path", "The repo settings changed meanwhile; try again.")
			default:
				maps.Copy(f, repoRefusal)
				repo = checked
			}
		}
		fetch := p.FetchMinutes
		if patch.FetchMinutes != nil {
			if m := *patch.FetchMinutes; m != 0 && (m < MinFetchMinutes || m > MaxFetchMinutes) {
				f.add("fetch_minutes", "Fetch every 5 to 1440 minutes, or 0 for never.")
			} else {
				fetch = m
			}
		}
		now := timefmt.Format(c.Now())
		var icon *Change
		if patch.IconKind != nil {
			glyph := ""
			if patch.IconGlyph != nil {
				glyph = *patch.IconGlyph
			}
			if icon, err = applyIcon(ctx, q, f, &p, *patch.IconKind, glyph, nil, now); err != nil {
				return err
			}
		}
		if err := f.err(); err != nil {
			return err
		}
		changes := map[string]Change{}
		if icon != nil {
			changes["icon"] = *icon
		}
		if name != p.Name {
			changes["name"] = Change{p.Name, name}
		}
		if description != p.Description {
			changes["description"] = Change{}
		}
		if workflow != (p.Workflow == 1) {
			changes["workflow"] = Change{p.Workflow == 1, workflow}
		}
		if tierMap != p.TierMap {
			changes["tier_map"] = Change{}
		}
		if repo.path != p.RepoPath {
			changes["repo_path"] = Change{p.RepoPath, repo.path}
		}
		if repo.main != p.MainBranch {
			changes["main_branch"] = Change{p.MainBranch, repo.main}
		}
		if repo.remote != p.Remote {
			changes["remote"] = Change{p.Remote, repo.remote}
		}
		if fetch != p.FetchMinutes {
			changes["fetch_minutes"] = Change{p.FetchMinutes, fetch}
		}
		if len(changes) == 0 {
			return nil
		}
		wf := int64(0)
		if workflow {
			wf = 1
		}
		if err := q.UpdateProject(ctx, gen.UpdateProjectParams{Name: name, Description: description, Workflow: wf, TierMap: tierMap,
			RepoPath: repo.path, MainBranch: repo.main, Remote: repo.remote, FetchMinutes: fetch, UpdatedAt: now, ID: p.ID}); err != nil {
			return fmt.Errorf("update project: %w", err)
		}
		p.Name, p.Description, p.Workflow, p.TierMap, p.UpdatedAt = name, description, wf, tierMap, now
		p.RepoPath, p.MainBranch, p.Remote, p.FetchMinutes = repo.path, repo.main, repo.remote, fetch
		return c.emit(ctx, q, by, EvProjectUpdated, p.ID, "", map[string]any{"changes": changes})
	})
	return p, err
}

// repoField is the first repo setting that want changes, as a form field.
func repoField(cur gen.Project, want repoSettings) string {
	switch {
	case want.path != cur.RepoPath:
		return "repo_path"
	case want.main != cur.MainBranch:
		return "main_branch"
	}
	return "remote"
}

// SetProjectArchived archives or restores a project.
func (c *Core) SetProjectArchived(ctx context.Context, by, projectID string, archived bool) error {
	return c.tx(ctx, func(q *gen.Queries) error {
		p, err := q.GetProject(ctx, projectID)
		if err != nil {
			return notFound(err, "get project")
		}
		if p.ArchivedAt.Valid == archived {
			if archived {
				return invalid("project", "It's already archived.")
			}
			return invalid("project", "It isn't archived.")
		}
		now := timefmt.Format(c.Now())
		at, kind := sql.NullString{}, EvProjectRestored
		if archived {
			at, kind = sql.NullString{String: now, Valid: true}, EvProjectArchived
		}
		if err := q.SetProjectArchived(ctx, gen.SetProjectArchivedParams{ArchivedAt: at, UpdatedAt: now, ID: p.ID}); err != nil {
			return fmt.Errorf("archive project: %w", err)
		}
		return c.emit(ctx, q, by, kind, p.ID, "", nil)
	})
}

// ChangeProjectKey gives a project a new key. Ticket numbers stay; the old
// key stops resolving (ticket links, branch names and commit references
// that use it break) and is free for another project.
func (c *Core) ChangeProjectKey(ctx context.Context, by, projectID, newKey string) (gen.Project, error) {
	var p gen.Project
	key := strings.ToUpper(strings.TrimSpace(newKey))
	err := c.tx(ctx, func(q *gen.Queries) error {
		var err error
		if p, err = q.GetProject(ctx, projectID); err != nil {
			return notFound(err, "get project")
		}
		switch {
		case !projectKeyRe.MatchString(key):
			return invalid("key", "Use 2 to 10 capital letters or digits, starting with a letter.")
		case key == p.Key:
			return invalid("key", "That's already this project's key.")
		}
		if _, err := q.GetProjectByKey(ctx, key); err == nil {
			return invalid("key", "That key is taken.")
		} else if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("check key: %w", err)
		}
		now := timefmt.Format(c.Now())
		if err := q.SetProjectKey(ctx, gen.SetProjectKeyParams{Key: key, UpdatedAt: now, ID: p.ID}); err != nil {
			return fmt.Errorf("set key: %w", err)
		}
		old := p.Key
		p.Key, p.UpdatedAt = key, now
		return c.emit(ctx, q, by, EvProjectUpdated, p.ID, "", map[string]any{"changes": map[string]Change{"key": {old, key}}})
	})
	return p, err
}

// DeleteProject removes a project and everything in it: its tickets, their
// tags, comments, plans (every revision) and links (including links from
// other projects' tickets), its decisions with their tickets and approvals,
// its icon, milestones, statuses and history, and the old keys of tickets
// that moved in or out (VAQ-18), and its project links either way round
// (VAQ-119). A ticket that moved away keeps its
// history: its events from its time here go with it to its project now.
// confirm must be the project's key
// (any case). One project.deleted event remains, with no project id.
// Callers take a backup first.
func (c *Core) DeleteProject(ctx context.Context, by, projectID, confirm string) error {
	return c.tx(ctx, func(q *gen.Queries) error {
		p, err := q.GetProject(ctx, projectID)
		if err != nil {
			return notFound(err, "get project")
		}
		if !strings.EqualFold(strings.TrimSpace(confirm), p.Key) {
			return invalid("confirm", "Type "+p.Key+" to confirm.")
		}
		steps := []struct {
			what string
			run  func(context.Context, string) error
		}{
			{"ticket tags", q.DeleteProjectTicketTags},
			{"comments", q.DeleteProjectComments},
			{"attachments", q.DeleteProjectAttachments},
			{"links", q.DeleteProjectLinks},
			{"references", q.DeleteProjectRefs},
			{"icon", q.DeleteProjectIcon},
			{"plan revisions", q.DeleteProjectPlanRevisions},
			{"plans", q.DeleteProjectPlans},
			{"approvals", q.DeleteProjectApprovals},
			{"decision tickets", q.DeleteProjectDecisionTickets},
			{"decisions", q.DeleteProjectDecisions},
			{"old keys", q.DeleteProjectOldKeys},
			{"project links", q.DeleteProjectProjectLinks},
			{"moved tickets' events", func(ctx context.Context, id string) error {
				return q.RehomeMovedTicketEvents(ctx, nullString(id))
			}},
			{"events' tickets", func(ctx context.Context, id string) error {
				return q.DeleteProjectEventTickets(ctx, nullString(id))
			}},
			{"events", func(ctx context.Context, id string) error {
				return q.DeleteProjectEvents(ctx, nullString(id))
			}},
			{"tickets", q.DeleteProjectTickets},
			{"milestones", q.DeleteProjectMilestones},
			{"statuses", q.DeleteProjectStatuses},
			{"project", q.DeleteProjectRow},
		}
		for _, s := range steps {
			if err := s.run(ctx, p.ID); err != nil {
				return fmt.Errorf("delete %s: %w", s.what, err)
			}
		}
		return c.emit(ctx, q, by, EvProjectDeleted, "", "", map[string]string{"key": p.Key, "name": p.Name})
	})
}
