package main

import (
	"context"
	"fmt"
	"io"
	"net/url"

	"github.com/stazelabs/vaquero/internal/api"
)

// cmdRef adds and removes a ticket's references (VAQ-136); `ticket show`
// lists them with their ids.
func cmdRef(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	const usage = "vaquero ref add TICKET KIND TARGET [--anchor A] [--label L] | remove TICKET ID"
	c := newCmd(stdout, stderr, usage)
	anchor := c.fs.String("anchor", "", "add: a heading anchor in the target")
	label := c.fs.String("label", "", "add: a name to show instead of the target")
	pos, err := c.parse(args, 1, 4)
	if err != nil {
		return c.finish(err)
	}
	switch {
	case pos[0] == "add" && len(pos) == 4:
		return c.connected(ctx, nil, func() error {
			key, err := canon(pos[1])
			if err != nil {
				return err
			}
			b := map[string]string{"kind": pos[2], "target": pos[3]}
			if *anchor != "" {
				b["anchor"] = *anchor
			}
			if *label != "" {
				b["label"] = *label
			}
			var ref api.Ref
			if err := c.call(ctx, "POST", "/api/v1/tickets/"+key+"/refs", b, &ref); err != nil {
				return err
			}
			c.emit(ref, func() { fmt.Fprintf(c.stdout, "Added a %s reference to %s.\n", ref.Kind, key) })
			return nil
		})
	case pos[0] == "remove" && len(pos) == 3:
		if err := c.only("remove"); err != nil {
			return c.finish(err)
		}
		return c.connected(ctx, nil, func() error {
			key, err := canon(pos[1])
			if err != nil {
				return err
			}
			if err := c.call(ctx, "DELETE", "/api/v1/tickets/"+key+"/refs/"+url.PathEscape(pos[2]), nil, nil); err != nil {
				return err
			}
			if !c.jsonOut {
				fmt.Fprintf(c.stdout, "Removed the reference from %s.\n", key)
			}
			return nil
		})
	}
	return c.finish(usageError{usage: usage})
}

// refWords is a reference as `ticket show` prints it: its id, kind, label
// (else target) and anchor.
func refWords(r api.Ref) string {
	s := r.Target
	if r.Anchor != "" {
		s += "#" + r.Anchor
	}
	if r.Label != "" {
		s = r.Label + " (" + s + ")"
	}
	return fmt.Sprintf("%s  %s  %s", r.ID, r.Kind, s)
}
