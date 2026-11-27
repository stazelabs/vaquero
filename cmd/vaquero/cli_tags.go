package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/core"
)

func init() {
	cliCommands["tags"] = cmdTags
	cliCommands["tag"] = cmdTag
}

// tagsPath is the tags path of project key, plus a tag's name.
func tagsPath(key string, name ...string) string {
	parts := []string{"/api/v1/projects", url.PathEscape(key), "tags"}
	for _, n := range name {
		parts = append(parts, url.PathEscape(n))
	}
	return strings.Join(parts, "/")
}

// ticketCount is "1 ticket" or "n tickets".
func ticketCount(n int64) string {
	if n == 1 {
		return "1 ticket"
	}
	return strconv.FormatInt(n, 10) + " tickets"
}

func cmdTags(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	c := newCmd(stdout, stderr, "vaquero tags KEY")
	pos, err := c.parse(args, 1, 1)
	return c.connected(ctx, err, func() error {
		var tags []api.Tag
		if err := c.call(ctx, "GET", tagsPath(pos[0]), nil, &tags); err != nil {
			return err
		}
		c.emit(tags, func() {
			rows := [][]string{{"NAME", "TICKETS"}}
			for _, t := range tags {
				rows = append(rows, []string{t.Name, strconv.FormatInt(t.TicketCount, 10)})
			}
			c.table(rows)
		})
		return nil
	})
}

func cmdTag(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	const usage = "vaquero tag edit KEY NAME --name NEW [--merge] | delete KEY NAME"
	c := newCmd(stdout, stderr, usage)
	newName := c.fs.String("name", "", "edit: the tag's new name")
	merge := c.fs.Bool("merge", false, "merge into the tag the project already has with the new name")
	pos, err := c.parse(args, 3, 3)
	if err != nil {
		return c.finish(err)
	}
	takes := map[string][]string{"edit": {"name", "merge"}}
	if err := c.only(pos[0], takes[pos[0]]...); err != nil {
		return c.finish(err)
	}
	key := pos[1]
	switch pos[0] {
	case "edit":
		if !c.set("name") {
			return c.finish(usageError{usage: usage, hint: "give the tag's new --name"})
		}
		// A rename onto a tag the project has is refused until --merge:
		// say how, not the API's field.
		c.printCurrent = func(json.RawMessage) { fmt.Fprintln(c.stderr, "Add --merge to merge the two.") }
		body := map[string]any{"name": *newName}
		if *merge {
			body["merge"] = true
		}
		return c.connected(ctx, nil, func() error {
			var t api.Tag
			if err := c.call(ctx, "PATCH", tagsPath(key, pos[2]), body, &t); err != nil {
				return err
			}
			c.emit(t, func() { fmt.Fprintf(c.stdout, "%s (%s)\n", t.Name, ticketCount(t.TicketCount)) })
			return nil
		})
	case "delete":
		if len(pos) != 3 {
			return c.finish(usageError{usage: usage})
		}
		return c.connected(ctx, nil, func() error {
			// The message names the tag as it is spelled, not as typed.
			name := pos[2]
			var tags []api.Tag
			if err := c.call(ctx, "GET", tagsPath(key), nil, &tags); err != nil {
				return err
			}
			for _, t := range tags {
				if core.SameTag(t.Name, strings.TrimSpace(name)) {
					name = t.Name
				}
			}
			if err := c.call(ctx, "DELETE", tagsPath(key, pos[2]), nil, nil); err != nil {
				return err
			}
			if !c.jsonOut {
				fmt.Fprintf(c.stdout, "Deleted tag %s from %s.\n", name, strings.ToUpper(key))
			}
			return nil
		})
	}
	return c.finish(usageError{usage: usage})
}
