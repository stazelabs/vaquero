package main

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"

	"github.com/stazelabs/vaquero/internal/api"
)

func init() {
	cliCommands["statuses"] = cmdStatuses
	cliCommands["status"] = cmdStatus
}

// categoryWords is a status category as the CLI shows it ("in progress").
func categoryWords(c string) string { return strings.ReplaceAll(c, "_", " ") }

// statusesPath is the statuses path of project key, plus a status's name.
func statusesPath(key string, name ...string) string {
	parts := []string{"/api/v1/projects", url.PathEscape(key), "statuses"}
	for _, n := range name {
		parts = append(parts, url.PathEscape(n))
	}
	return strings.Join(parts, "/")
}

func cmdStatuses(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	c := newCmd(stdout, stderr, "vaquero statuses KEY")
	pos, err := c.parse(args, 1, 1)
	return c.connected(ctx, err, func() error {
		var ss []api.Status
		if err := c.call(ctx, "GET", statusesPath(pos[0]), nil, &ss); err != nil {
			return err
		}
		c.emit(ss, func() {
			rows := [][]string{{"NAME", "CATEGORY", "TICKETS"}}
			for _, s := range ss {
				rows = append(rows, []string{s.Name, categoryWords(s.Category), strconv.FormatInt(s.TicketCount, 10)})
			}
			c.table(rows)
		})
		return nil
	})
}

func cmdStatus(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	const usage = "vaquero status add KEY NAME --category C [--before S | --after S] | rename KEY NAME NEW | move KEY NAME --before S | --after S | delete KEY NAME"
	c := newCmd(stdout, stderr, usage)
	category := c.fs.String("category", "", "the new status's category")
	before := c.fs.String("before", "", "the status it goes before")
	after := c.fs.String("after", "", "the status it goes after")
	pos, err := c.parse(args, 3, 4)
	if err != nil {
		return c.finish(err)
	}
	if c.set("before") && c.set("after") {
		return c.finish(usageError{usage: usage, hint: "give --before or --after, not both"})
	}
	var s api.Status
	call := func(method, path string, body any) int {
		return c.connected(ctx, nil, func() error {
			if err := c.call(ctx, method, path, body, &s); err != nil {
				return err
			}
			c.emit(s, func() { fmt.Fprintf(c.stdout, "%s (%s), position %d\n", s.Name, categoryWords(s.Category), s.Position) })
			return nil
		})
	}
	place := map[string]string{}
	if c.set("before") {
		place["before"] = *before
	}
	if c.set("after") {
		place["after"] = *after
	}
	takes := map[string][]string{"add": {"category", "before", "after"}, "move": {"before", "after"}}
	if err := c.only(pos[0], takes[pos[0]]...); err != nil {
		return c.finish(err)
	}
	key := pos[1]
	switch pos[0] {
	case "add":
		if len(pos) != 3 {
			return c.finish(usageError{usage: usage})
		}
		if !c.set("category") {
			return c.finish(usageError{usage: usage, hint: "give the new status's --category"})
		}
		place["name"], place["category"] = pos[2], *category
		return call("POST", statusesPath(key), place)
	case "rename":
		if len(pos) != 4 {
			return c.finish(usageError{usage: usage})
		}
		return call("PATCH", statusesPath(key, pos[2]), map[string]string{"name": pos[3]})
	case "move":
		if len(pos) != 3 || len(place) == 0 {
			return c.finish(usageError{usage: usage})
		}
		return call("POST", statusesPath(key, pos[2], "move"), place)
	case "delete":
		if len(pos) != 3 {
			return c.finish(usageError{usage: usage})
		}
		return c.connected(ctx, nil, func() error {
			if err := c.call(ctx, "DELETE", statusesPath(key, pos[2]), nil, nil); err != nil {
				return err
			}
			if !c.jsonOut {
				fmt.Fprintf(c.stdout, "Deleted status %s from %s.\n", pos[2], strings.ToUpper(key))
			}
			return nil
		})
	}
	return c.finish(usageError{usage: usage})
}
