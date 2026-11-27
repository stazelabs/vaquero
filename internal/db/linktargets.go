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
	return runBackfill(ctx, tx, linkTargetsMigration, "link targets")
}

// eventTicketsMigration holds the event_tickets backfill between its
// markers.
const eventTicketsMigration = "migrations/0013_history_profile.sql"

// BackfillEventTickets gives every event whose payload.ticket_ids is a list
// its event_tickets rows, the lookup a ticket's history reads (ruling
// P-R1), adding only those missing. Migration 0013 ran it on every
// database it upgraded; an import runs it after loading, since the table
// isn't exported. Its statement is read from the migration, so both always
// do the same.
func BackfillEventTickets(ctx context.Context, tx *sql.Tx) error {
	return runBackfill(ctx, tx, eventTicketsMigration, "event tickets")
}

// approvalsMovedMigration holds the moved-ticket approval cleanup between
// its markers.
const approvalsMovedMigration = "migrations/0014_approvals_moved.sql"

// ClearMovedApprovals applies the move rule to approvals written before
// it (ruling R-Q13): it deletes each approval asked before its ticket's
// latest move to another project, and takes each descendant moved after
// an approval was asked out of that approval's pinned children, keeping
// every other pin field. Migration 0014 ran it on every database it
// upgraded; an import runs it after loading an export from a release
// before that. Its statements are read from the migration, so both always
// do the same.
func ClearMovedApprovals(ctx context.Context, tx *sql.Tx) error {
	return runBackfill(ctx, tx, approvalsMovedMigration, "moved approvals")
}

// runBackfill runs a migration's backfill statements through tx.
func runBackfill(ctx context.Context, tx *sql.Tx, migration, what string) error {
	stmts, err := backfillOf(migration)
	if err != nil {
		return err
	}
	for _, s := range stmts {
		if _, err := tx.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("backfill %s: %w", what, err)
		}
	}
	return nil
}

// backfillOf is a migration's backfill statements, as it has them between
// its "-- backfill:begin" and "-- backfill:end" lines.
func backfillOf(migration string) ([]string, error) {
	raw, err := migrationsFS.ReadFile(migration)
	if err != nil {
		return nil, err
	}
	_, rest, ok := strings.Cut(string(raw), "-- backfill:begin\n")
	body, _, ok2 := strings.Cut(rest, "-- backfill:end\n")
	if !ok || !ok2 {
		return nil, fmt.Errorf("%s has no backfill markers", migration)
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
