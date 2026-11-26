package board

import (
	"cmp"
	"context"
	"slices"

	"github.com/stazelabs/vaquero/internal/db/gen"
)

// ProjectLink is one of a project's links as that project sees it
// (VAQ-119): Kind is "depends on" or "relates"; Direction is "out" when
// this project is the link's from end (it depends on Key, or relates to
// it), "in" when Key is (Key depends on this project, which reads "used
// by"); Key and Name are the other project's.
type ProjectLink struct {
	ID, Kind, Direction, Key, Name string
}

// LoadProjectLinks reads every project's links, by project id, in one
// query: each link is listed under both its projects. Each project's links
// come in the order the pages show them: what it depends on, what depends
// on it, then what it relates to, each by key.
func LoadProjectLinks(ctx context.Context, q *gen.Queries) (map[string][]ProjectLink, error) {
	rows, err := q.ListAllProjectLinks(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string][]ProjectLink{}
	for _, r := range rows {
		out[r.FromProject] = append(out[r.FromProject], ProjectLink{ID: r.ID, Kind: r.Kind, Direction: "out", Key: r.ToKey, Name: r.ToName})
		out[r.ToProject] = append(out[r.ToProject], ProjectLink{ID: r.ID, Kind: r.Kind, Direction: "in", Key: r.FromKey, Name: r.FromName})
	}
	for _, ls := range out {
		slices.SortFunc(ls, func(a, b ProjectLink) int {
			return cmp.Or(cmp.Compare(linkRank(a), linkRank(b)), cmp.Compare(a.Key, b.Key), cmp.Compare(a.ID, b.ID))
		})
	}
	return out, nil
}

// linkRank orders a project's links: depends on, used by, relates.
func linkRank(l ProjectLink) int {
	switch {
	case l.Kind == "depends on" && l.Direction == "out":
		return 0
	case l.Kind == "depends on":
		return 1
	}
	return 2
}
