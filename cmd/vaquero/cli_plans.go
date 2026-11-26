package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/stazelabs/vaquero/internal/api"
)

// cmdPlan reads and writes a ticket's plan: plan show prints the current
// revision (or --revision N), plan save sends a whole new text from a file
// or standard input. Saving over an existing plan needs --revision, the
// revision read; a stale one exits 3 and prints the plan as it is now.
func cmdPlan(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	const usage = "vaquero plan show TICKET [--revision N] | save TICKET FILE|- [--revision N]"
	c := newCmd(stdout, stderr, usage)
	revision := c.fs.Int64("revision", 0, "show: that revision; save: the revision you read (leave it out for a new plan)")
	pos, err := c.parse(args, 2, 3)
	if err != nil {
		return c.finish(err)
	}
	if c.set("revision") && *revision < 1 {
		return c.finish(usageError{usage: usage, hint: "--revision is a plan revision, 1 or more"})
	}
	var p api.Plan
	c.printCurrent = func(raw json.RawMessage) {
		var cur api.Plan
		if json.Unmarshal(raw, &cur) == nil {
			printPlan(c.stdout, cur)
		}
	}
	switch pos[0] {
	case "show":
		if len(pos) != 2 {
			return c.finish(usageError{usage: usage})
		}
		return c.connected(ctx, nil, func() error {
			key, err := canon(pos[1])
			if err != nil {
				return err
			}
			path := "/api/v1/tickets/" + key + "/plan"
			if *revision > 0 {
				path += "/revisions/" + strconv.FormatInt(*revision, 10)
			}
			if err := c.call(ctx, "GET", path, nil, &p); err != nil {
				return err
			}
			c.emit(p, func() { printPlan(c.stdout, p) })
			return nil
		})
	case "save":
		if len(pos) != 3 {
			return c.finish(usageError{usage: usage})
		}
		return c.connected(ctx, nil, func() error {
			key, err := canon(pos[1])
			if err != nil {
				return err
			}
			body, err := planText(pos[2])
			if err != nil {
				return err
			}
			b := map[string]any{"body": body}
			if *revision > 0 {
				b["revision"] = *revision
			}
			if err := c.call(ctx, "PUT", "/api/v1/tickets/"+key+"/plan", b, &p); err != nil {
				return err
			}
			c.emit(p, func() { fmt.Fprintf(c.stdout, "Saved the plan for %s (revision %d).\n", p.Ticket, p.Revision) })
			return nil
		})
	}
	return c.finish(usageError{usage: usage})
}

// planText is a plan's text: standard input for "-", else the named file.
func planText(arg string) (string, error) {
	if arg == "-" {
		return text(arg)
	}
	b, err := os.ReadFile(arg)
	if err != nil {
		return "", message("Can't read " + arg + ".")
	}
	return string(b), nil
}

// printPlan writes a plan's revision line, then its text.
func printPlan(w io.Writer, p api.Plan) {
	fmt.Fprintf(w, "%s plan · Revision %d · %s · %s\n\n%s\n", p.Ticket, p.Revision, p.UpdatedBy, p.UpdatedAt, p.Body)
}
