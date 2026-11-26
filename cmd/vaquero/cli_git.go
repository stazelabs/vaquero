package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/timefmt"
	"github.com/stazelabs/vaquero/internal/units"
)

// The repository reads (VAQ-136): a file at main or a revision, and the
// commits on main that name a ticket.
func init() {
	cliCommands["file"] = cmdFile
	cliCommands["commits"] = cmdCommits
	cliCommands["fetch"] = cmdFetch
}

// cmdFetch fetches a project's remote now: the one command that contacts a
// remote. A failed fetch exits 1 with git's first line.
func cmdFetch(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	c := newCmd(stdout, stderr, "vaquero fetch KEY")
	pos, err := c.parse(args, 1, 1)
	return c.connected(ctx, err, func() error {
		key := strings.ToUpper(pos[0])
		var res api.FetchResult
		if err := c.call(ctx, "POST", "/api/v1/projects/"+url.PathEscape(key)+"/repo/fetch", nil, &res); err != nil {
			return err
		}
		if c.jsonOut {
			c.emit(res, nil)
		}
		if res.Error != "" {
			return errors.New("Fetch failed: " + res.Error)
		}
		if !c.jsonOut {
			fmt.Fprintf(c.stdout, "Fetched %s.\n", key)
		}
		return nil
	})
}

// cmdFile prints a file from a project's repository as it is, text only:
// a file too large to show or not text is an error naming why.
func cmdFile(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	const usage = "vaquero file KEY PATH [--at REV]"
	c := newCmd(stdout, stderr, usage)
	at := c.fs.String("at", "", "a branch, tag or commit hash (default: the main branch)")
	pos, err := c.parse(args, 2, 2)
	return c.connected(ctx, err, func() error {
		path := "/api/v1/projects/" + url.PathEscape(strings.ToUpper(pos[0])) + "/files/" + (&url.URL{Path: pos[1]}).EscapedPath()
		if c.set("at") {
			path += "?" + url.Values{"at": {*at}}.Encode()
		}
		var f api.File
		if err := c.call(ctx, "GET", path, nil, &f); err != nil {
			return err
		}
		if c.jsonOut {
			c.emit(f, nil)
			return nil
		}
		switch {
		case f.TooLarge:
			return fmt.Errorf("%s is too large to show (%s)", f.Path, units.SizeWords(f.Size))
		case f.Text == nil:
			return fmt.Errorf("%s isn't text", f.Path)
		}
		_, err := io.WriteString(c.stdout, *f.Text)
		return err
	})
}

// cmdCommits lists the commits on main that name a ticket, newest first,
// with their diffstat and the release that first contains them.
func cmdCommits(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	c := newCmd(stdout, stderr, "vaquero commits TICKET")
	pos, err := c.parse(args, 1, 1)
	return c.connected(ctx, err, func() error {
		key, err := canon(pos[0])
		if err != nil {
			return err
		}
		var tc api.TicketCommits
		if err := c.call(ctx, "GET", "/api/v1/tickets/"+key+"/commits", nil, &tc); err != nil {
			return err
		}
		c.emit(tc, func() {
			if len(tc.Commits) == 0 {
				fmt.Fprintf(c.stdout, "No commits mention %s on main.\n", key)
				return
			}
			for _, cm := range tc.Commits {
				fmt.Fprintf(c.stdout, "%s  %s  %s  (%s)\n", cm.Short, day(cm.At), cm.Subject, cm.Author)
			}
			files := "1 file"
			if n := len(tc.Diffstat.Files); n != 1 {
				files = fmt.Sprintf("%d files", n)
			}
			fmt.Fprintf(c.stdout, "\n+%d −%d in %s\n", tc.Diffstat.Added, tc.Diffstat.Removed, files)
			if tc.Released != "" {
				fmt.Fprintf(c.stdout, "Released in %s.\n", tc.Released)
			}
		})
		return nil
	})
}

// day is an instant's date, for a commit line.
func day(at string) string {
	t, err := timefmt.Parse(at)
	if err != nil {
		return at
	}
	return timefmt.DisplayDate(t, nil)
}
