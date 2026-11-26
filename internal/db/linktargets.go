package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// linkTargetsMigration holds the link-target backfill between its
// "-- backfill:begin" and "-- backfill:end" lines.
const linkTargetsMigration = "migrations/0008_link_targets.sql"

// BackfillLinkTargets gives link events without a payload to_id the target
// ticket's id, where it can be known for sure: from the link row while it
// exists, else from the key, when the ticket it names existed at the time
// and its project has kept that key since. The rest keep no to_id, and a
// ticket's history no longer lists them on the target's side.
//
// Migration 0008 ran it on every database it upgraded; an import runs it
// after loading an export from a release before that. Its statements are
// read from the migration, so both always do the same.
func BackfillLinkTargets(ctx context.Context, tx *sql.Tx) error {
	stmts, err := linkTargetsBackfill()
	if err != nil {
		return err
	}
	for _, s := range stmts {
		if _, err := tx.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("backfill link targets: %w", err)
		}
	}
	return nil
}

// linkTargetsBackfill is the backfill's statements, as migration 0008 has
// them.
func linkTargetsBackfill() ([]string, error) {
	raw, err := migrationsFS.ReadFile(linkTargetsMigration)
	if err != nil {
		return nil, err
	}
	_, rest, ok := strings.Cut(string(raw), "-- backfill:begin\n")
	body, _, ok2 := strings.Cut(rest, "-- backfill:end\n")
	if !ok || !ok2 {
		return nil, fmt.Errorf("%s has no backfill markers", linkTargetsMigration)
	}
	var out []string
	for s := range strings.SplitSeq(body, ";\n") {
		var code []string
		for line := range strings.SplitSeq(s, "\n") {
			if t := strings.TrimSpace(line); t != "" && !strings.HasPrefix(t, "--") {
				code = append(code, line)
			}
		}
		if len(code) > 0 {
			out = append(out, strings.Join(code, "\n"))
		}
	}
	return out, nil
}
