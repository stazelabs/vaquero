package main

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/stazelabs/vaquero/internal/api"
)

func init() {
	cliCommands["milestones"] = cmdMilestones
	cliCommands["milestone"] = cmdMilestone
}

// milestonesPath is the milestones path of project key, plus a
// milestone's name and an action.
func milestonesPath(key string, rest ...string) string {
	parts := []string{"/api/v1/projects", url.PathEscape(key), "milestones"}
	for _, r := range rest {
		parts = append(parts, url.PathEscape(r))
	}
	return strings.Join(parts, "/")
}

func milestoneState(m api.Milestone) string {
	if m.Closed {
		return "closed"
	}
	return "open"
}

func cmdMilestones(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	c := newCmd(stdout, stderr, "vaquero milestones KEY")
	pos, err := c.parse(args, 1, 1)
	return c.connected(ctx, err, func() error {
		var ms []api.Milestone
		if err := c.call(ctx, "GET", milestonesPath(pos[0]), nil, &ms); err != nil {
			return err
		}
		c.emit(ms, func() {
			if len(ms) == 0 {
				fmt.Fprintf(c.stdout, "No milestones in %s.\n", strings.ToUpper(pos[0]))
				return
			}
			rows := [][]string{{"NAME", "DUE", "DONE", "STATE"}}
			for _, m := range ms {
				due := m.DueOn
				if due == "" {
					due = "-"
				}
				rows = append(rows, []string{m.Name, due, fmt.Sprintf("%d/%d", m.Done, m.Total), milestoneState(m)})
			}
			c.table(rows)
		})
		return nil
	})
}

func cmdMilestone(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	const usage = "vaquero milestone add KEY NAME [--description TEXT|-] [--due DATE] | edit KEY NAME [--name NEW] [--description TEXT|-] [--due DATE] | close KEY NAME | reopen KEY NAME"
	c := newCmd(stdout, stderr, usage)
	name := c.fs.String("name", "", "new name")
	desc := c.fs.String("description", "", "description (- reads standard input)")
	due := c.fs.String("due", "", "due date, YYYY-MM-DD (\"\" for none)")
	pos, err := c.parse(args, 3, 3)
	if err != nil {
		return c.finish(err)
	}
	takes := map[string][]string{"add": {"description", "due"}, "edit": {"name", "description", "due"}}
	if err := c.only(pos[0], takes[pos[0]]...); err != nil {
		return c.finish(err)
	}
	key, ms := pos[1], pos[2]
	call := func(method, path string, body map[string]string) int {
		return c.connected(ctx, nil, func() error {
			if body != nil && c.set("description") {
				d, err := text(*desc)
				if err != nil {
					return err
				}
				body["description"] = d
			}
			var m api.Milestone
			if err := c.call(ctx, method, path, body, &m); err != nil {
				return err
			}
			c.emit(m, func() {
				line := fmt.Sprintf("%s (%s), %d of %d done", m.Name, milestoneState(m), m.Done, m.Total)
				if m.DueOn != "" {
					line += ", due " + m.DueOn
				}
				fmt.Fprintln(c.stdout, line)
			})
			return nil
		})
	}
	body := map[string]string{}
	if c.set("due") {
		body["due_on"] = *due
	}
	switch pos[0] {
	case "add":
		body["name"] = ms
		return call("POST", milestonesPath(key), body)
	case "edit":
		if c.set("name") {
			body["name"] = *name
		}
		return call("PATCH", milestonesPath(key, ms), body)
	case "close":
		return call("POST", milestonesPath(key, ms, "close"), nil)
	case "reopen":
		return call("POST", milestonesPath(key, ms, "reopen"), nil)
	}
	return c.finish(usageError{usage: usage})
}
