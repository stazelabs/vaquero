package core

import "testing"

// SetMaxLoopWalk bounds the loop message's walk for one test.
func SetMaxLoopWalk(t testing.TB, n int) {
	t.Helper()
	saved := maxLoopWalk
	maxLoopWalk = n
	t.Cleanup(func() { maxLoopWalk = saved })
}
