package templates

import (
	"context"
	"strings"
	"testing"
)

// The empty state sits inside the live #feed part, so the first live event
// replaces it (VAQ-44).
func TestActivityEmptyStateIsLive(t *testing.T) {
	var b strings.Builder
	if err := ActivityPage(nil).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	page := b.String()
	feed := strings.Index(page, `id="feed"`)
	empty := strings.Index(page, "Nothing has happened yet.")
	if feed < 0 || empty < feed {
		t.Fatalf("the empty state is outside #feed (feed at %d, empty state at %d)", feed, empty)
	}
}
