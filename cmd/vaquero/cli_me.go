package main

import (
	"context"
	"fmt"
	"io"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/core"
)

func init() {
	cliCommands["me"] = cmdMe
}

// cmdMe is vaquero me (VAQ-149, ruling P-R3; "profile" names connection
// profiles): the owner's profile, and with flags a change to it. Only the
// owner, not an agent, changes it.
func cmdMe(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	const usage = "vaquero me [--name NAME] [--avatar FILE | --remove-avatar]"
	c := newCmd(stdout, stderr, usage)
	name := c.fs.String("name", "", "display name; empty shows Owner again")
	avatar := c.fs.String("avatar", "", "a PNG, JPEG or WebP image of up to 256 KB")
	c.fs.Bool("remove-avatar", false, "remove the avatar (the monogram shows again)")
	_, err := c.parse(args, 0, 0)
	if err == nil && c.set("avatar") && c.set("remove-avatar") {
		err = usageError{usage: usage, hint: "--avatar and --remove-avatar don't go together"}
	}
	return c.connected(ctx, err, func() error {
		var p api.Profile
		if c.set("name") {
			if err := c.call(ctx, "PATCH", "/api/v1/profile", map[string]string{"display_name": *name}, &p); err != nil {
				return err
			}
		}
		switch {
		case c.set("avatar"):
			if err := uploadImage(ctx, c, "/api/v1/profile/avatar", *avatar, &p); err != nil {
				return err
			}
		case c.set("remove-avatar"):
			if err := c.call(ctx, "DELETE", "/api/v1/profile/avatar", nil, &p); err != nil {
				return err
			}
		case !c.set("name"):
			if err := c.call(ctx, "GET", "/api/v1/profile", nil, &p); err != nil {
				return err
			}
		}
		c.emit(p, func() {
			fmt.Fprintf(c.stdout, "Name: %s\n", p.Shown)
			if p.Avatar != nil {
				fmt.Fprintln(c.stdout, "Avatar: image")
			} else {
				fmt.Fprintf(c.stdout, "Avatar: monogram %s\n", core.Monogram(p.Shown, p.Shown))
			}
		})
		return nil
	})
}
