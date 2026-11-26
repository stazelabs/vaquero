package main

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/timefmt"
)

func init() {
	cliCommands["ready"] = cmdReady
	cliCommands["claim"] = cmdClaimAction("claim", true)
	cliCommands["heartbeat"] = cmdClaimAction("heartbeat", true)
	cliCommands["release"] = cmdClaimAction("release", false)
}

// cmdReady lists the ready queue.
func cmdReady(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	const usage = "vaquero ready [KEY] [--limit N]"
	c := newCmd(stdout, stderr, usage)
	limit := c.fs.Int("limit", 20, "at most this many")
	pos, err := c.parse(args, 0, 1)
	if err == nil && *limit < 1 {
		err = usageError{usage: usage, hint: "--limit is 1 or more"}
	}
	return c.connected(ctx, err, func() error {
		q := url.Values{"limit": {strconv.Itoa(*limit)}}
		if len(pos) == 1 {
			q.Set("project", pos[0])
		}
		var ts []api.TicketSummary
		if err := c.call(ctx, "GET", "/api/v1/ready?"+q.Encode(), nil, &ts); err != nil {
			return err
		}
		c.emit(ts, func() {
			if len(ts) == 0 {
				fmt.Fprintln(c.stdout, "Nothing is ready.")
				return
			}
			// A tier column only when there are tiers: with the agent
			// workflow off, summaries carry none.
			tiers := slices.ContainsFunc(ts, func(t api.TicketSummary) bool { return t.Tier != "" })
			rows := [][]string{{"KEY", "PRIORITY", "TITLE", "TAGS"}}
			if tiers {
				rows[0] = append(rows[0], "TIER")
			}
			for _, t := range ts {
				row := []string{t.Key, priorityWords[t.Priority], t.Title, strings.Join(t.Tags, ",")}
				if tiers {
					row = append(row, t.Tier)
				}
				rows = append(rows, row)
			}
			c.table(rows)
		})
		return nil
	})
}

// cmdClaimAction is vaquero claim, heartbeat or release: verb TICKET, with
// --lease for the first two.
func cmdClaimAction(verb string, withLease bool) func(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	return func(ctx context.Context, stdout, stderr io.Writer, args []string) int {
		usage := "vaquero " + verb + " TICKET"
		if withLease {
			usage += " [--lease 30m]"
		}
		c := newCmd(stdout, stderr, usage)
		var lease *time.Duration
		if withLease {
			lease = c.fs.Duration("lease", 0, "how long to hold it, 1m to 2h (default 15m)")
		}
		pos, err := c.parse(args, 1, 1)
		var body map[string]int64
		if err == nil && withLease && c.set("lease") {
			if *lease < time.Minute || *lease%time.Minute != 0 {
				err = usageError{usage: usage, hint: "--lease is whole minutes, from 1m to 2h"}
			} else {
				body = map[string]int64{"lease_minutes": int64(*lease / time.Minute)}
			}
		}
		return c.connected(ctx, err, func() error {
			key, err := canon(pos[0])
			if err != nil {
				return err
			}
			var t apiTicket
			var b any
			if body != nil {
				b = body
			}
			if err := c.call(ctx, "POST", "/api/v1/tickets/"+key+"/"+verb, b, &t); err != nil {
				return err
			}
			c.emit(t, func() {
				if verb == "release" {
					fmt.Fprintf(c.stdout, "Released %s.\n", t.Key)
					return
				}
				fmt.Fprintf(c.stdout, "%s is claimed by %s until %s.\n", t.Key, t.ClaimedBy, untilWords(t.ClaimExpiresAt))
			})
			return nil
		})
	}
}

// untilWords is a lease's end as people read it.
func untilWords(stamp string) string {
	t, err := timefmt.Parse(stamp)
	if err != nil {
		return stamp
	}
	return timefmt.DisplayUTC(t)
}
