package db_test

import (
	"strings"
	"testing"

	appdb "github.com/stazelabs/vaquero/internal/db"
	"github.com/stazelabs/vaquero/internal/db/dbtest"
)

// VAQ-133: comment snippets are made in one pass over the comment index,
// keyed by ticket, not by a MATCH run again for each kept row. The plan
// may hold one correlated subquery, the description's snippet (an FTS5
// rowid lookup), and none over comments_fts.
func TestSearchCommentSnippetsInOnePass(t *testing.T) {
	d := dbtest.New(t)
	for n := 1; n <= 3; n++ {
		args := make([]any, 0, 3*n+3)
		for range n {
			args = append(args, `title : "x"*`, `body : "x"*`, `"x"*`)
		}
		args = append(args, int64(500), `("x"*)`, `body : ("x"*)`)
		rows, err := d.R.QueryContext(ctx, "EXPLAIN QUERY PLAN "+appdb.SearchSQL(n), args...)
		if err != nil {
			t.Fatal(err)
		}
		var plan []string
		for rows.Next() {
			var id, parent, notused int
			var detail string
			if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
				t.Fatal(err)
			}
			plan = append(plan, detail)
		}
		rows.Close()
		correlated := 0
		for i, line := range plan {
			if !strings.HasPrefix(line, "CORRELATED") {
				continue
			}
			correlated++
			for _, next := range plan[i+1 : min(i+3, len(plan))] {
				if strings.Contains(next, "comments_fts") {
					t.Fatalf("%d terms: a correlated subquery reads comments_fts:\n%s", n, strings.Join(plan, "\n"))
				}
			}
		}
		// One scan of the comment index per term, and one for the snippets.
		if got := strings.Count(strings.Join(plan, "\n"), "SCAN comments_fts"); got != n+1 {
			t.Fatalf("%d terms: %d scans of comments_fts, want %d:\n%s", n, got, n+1, strings.Join(plan, "\n"))
		}
		if correlated > 1 {
			t.Fatalf("%d terms: %d correlated subqueries, want at most 1:\n%s", n, correlated, strings.Join(plan, "\n"))
		}
	}
}
