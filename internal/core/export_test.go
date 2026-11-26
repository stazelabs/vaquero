package core

import (
	"context"
	"testing"

	"github.com/stazelabs/vaquero/internal/db/gen"
)

// SetMaxLoopWalk bounds the loop message's walk for one test.
func SetMaxLoopWalk(t testing.TB, n int) {
	t.Helper()
	saved := maxLoopWalk
	maxLoopWalk = n
	t.Cleanup(func() { maxLoopWalk = saved })
}

// SetInOpenRun replaces the open-run check until restore is called.
func SetInOpenRun(fn func(context.Context, *gen.Queries, []string) (bool, error)) (restore func()) {
	saved := inOpenRun
	inOpenRun = fn
	return func() { inOpenRun = saved }
}
