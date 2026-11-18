package core_test

import (
	"testing"

	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/db/dbtest"
	"github.com/stazelabs/vaquero/internal/db/gen"
)

// fixture is the shared starting state for operation tests. Task 3 fills in
// a project, its statuses, tickets, a tag and a milestone.
type fixture struct {
	Owner gen.Actor
}

func newFixture(t *testing.T) (*core.Core, dbtest.DB, fixture) {
	t.Helper()
	c, d, owner := newCore(t)
	return c, d, fixture{Owner: owner}
}
