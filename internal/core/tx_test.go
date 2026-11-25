package core

import (
	"context"
	"testing"
	"time"

	"github.com/stazelabs/vaquero/internal/db/dbtest"
	"github.com/stazelabs/vaquero/internal/db/gen"
)

// A panic inside an operation must not keep the writer's only connection.
func TestPanicInOperationReleasesWriter(t *testing.T) {
	d := dbtest.New(t)
	c := New(d.W, time.Now)
	func() {
		defer func() { _ = recover() }()
		_ = c.tx(context.Background(), func(*gen.Queries) error { panic("boom") })
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.tx(ctx, func(*gen.Queries) error { return nil }); err != nil {
		t.Fatalf("writer unusable after a panic: %v", err)
	}
}
