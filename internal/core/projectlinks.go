package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/stazelabs/vaquero/internal/db/gen"
	"github.com/stazelabs/vaquero/internal/idgen"
	"github.com/stazelabs/vaquero/internal/timefmt"
)

// ProjectLinkKinds are the ways one project can point at another (VAQ-119):
// it "depends on" the other (the other provides what it builds on), or the
// two "relate". Links only describe: they gate nothing, and ticket-level
// "blocks" across projects stays the way to say one piece of work waits
// on another.
var ProjectLinkKinds = []string{"depends on", "relates"}

// AddProjectLink links the project fromProjectID to the one keyed toKey
// (any case). kind is a ProjectLinkKinds entry; "depends-on" is taken for
// "depends on". Refused: a link to itself, a repeat ("relates" either way
// round), and a "depends on" that would close a loop, directly or through
// a chain. One project.linked event, on the from project.
func (c *Core) AddProjectLink(ctx context.Context, by, fromProjectID, kind, toKey string) (gen.ProjectLink, error) {
	var l gen.ProjectLink
	kind = strings.ReplaceAll(strings.TrimSpace(kind), "-", " ")
	key := strings.ToUpper(strings.TrimSpace(toKey))
	err := c.tx(ctx, func(q *gen.Queries) error {
		from, err := q.GetProject(ctx, fromProjectID)
		if err != nil {
			return notFound(err, "get project")
		}
		f := fields{}
		if !slices.Contains(ProjectLinkKinds, kind) {
			f.add("kind", "Choose depends on or relates.")
		}
		var to gen.Project
		if key == "" {
			f.add("project", "Choose a project.")
		} else if to, err = q.GetProjectByKey(ctx, key); errors.Is(err, sql.ErrNoRows) {
			f.add("project", "No project "+key+".")
		} else if err != nil {
			return fmt.Errorf("get project: %w", err)
		} else if to.ID == from.ID {
			f.add("project", "A project can't link to itself.")
		}
		if err := f.err(); err != nil {
			return err
		}
		all, err := q.ListAllProjectLinks(ctx)
		if err != nil {
			return fmt.Errorf("list project links: %w", err)
		}
		for _, x := range all {
			if x.Kind != kind {
				continue
			}
			same := x.FromProject == from.ID && x.ToProject == to.ID
			switch {
			case kind == "relates" && (same || x.FromProject == to.ID && x.ToProject == from.ID):
				return invalid("project", from.Key+" and "+to.Key+" are already related.")
			case same:
				return invalid("project", from.Key+" already depends on "+to.Key+".")
			}
		}
		if kind == "depends on" {
			if err := dependsLoop(all, from, to); err != nil {
				return err
			}
		}
		l, err = q.CreateProjectLink(ctx, gen.CreateProjectLinkParams{
			ID: idgen.New(), FromProject: from.ID, ToProject: to.ID, Kind: kind,
			CreatedAt: timefmt.Format(c.Now()), CreatedBy: by,
		})
		if err != nil {
			return fmt.Errorf("create project link: %w", err)
		}
		return c.emit(ctx, q, by, EvProjectLinked, from.ID, "", projectLinkPayload(l, to.Key))
	})
	return l, err
}

// dependsLoop refuses from depending on to when to already depends on
// from, directly or through a chain of "depends on" links, naming the
// chain. Projects are few, so it walks every link in memory.
func dependsLoop(all []gen.ListAllProjectLinksRow, from, to gen.Project) error {
	next := map[string][]gen.ListAllProjectLinksRow{}
	for _, x := range all {
		if x.Kind == "depends on" {
			next[x.FromProject] = append(next[x.FromProject], x)
		}
	}
	type step struct {
		id, key string
		prev    int
	}
	queue := []step{{id: to.ID, key: to.Key, prev: -1}}
	seen := map[string]bool{to.ID: true}
	for i := 0; i < len(queue); i++ {
		for _, x := range next[queue[i].id] {
			if x.ToProject == from.ID {
				var via []string
				for j := i; j > 0; j = queue[j].prev {
					via = append([]string{queue[j].key}, via...)
				}
				msg := "That would make " + from.Key + " depend on itself: " + to.Key
				if len(via) == 0 {
					return invalid("project", msg+" already depends on "+from.Key+".")
				}
				return invalid("project", msg+" depends on "+from.Key+" through "+strings.Join(via, ", ")+".")
			}
			if !seen[x.ToProject] {
				seen[x.ToProject] = true
				queue = append(queue, step{id: x.ToProject, key: x.ToKey, prev: i})
			}
		}
	}
	return nil
}

// RemoveProjectLink deletes a link of projectID's, either way round; a
// link that doesn't touch projectID is ErrNotFound. One project.unlinked
// event, on the link's from project.
func (c *Core) RemoveProjectLink(ctx context.Context, by, projectID, linkID string) error {
	return c.tx(ctx, func(q *gen.Queries) error {
		l, err := q.GetProjectLink(ctx, linkID)
		if err != nil {
			return notFound(err, "get project link")
		}
		if l.FromProject != projectID && l.ToProject != projectID {
			return ErrNotFound
		}
		to, err := q.GetProject(ctx, l.ToProject)
		if err != nil {
			return fmt.Errorf("get project: %w", err)
		}
		if err := q.DeleteProjectLink(ctx, l.ID); err != nil {
			return fmt.Errorf("delete project link: %w", err)
		}
		return c.emit(ctx, q, by, EvProjectUnlinked, l.FromProject, "", projectLinkPayload(l, to.Key))
	})
}

func projectLinkPayload(l gen.ProjectLink, toKey string) map[string]string {
	return map[string]string{"link_id": l.ID, "kind": l.Kind, "to": toKey, "to_id": l.ToProject}
}
