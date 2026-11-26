package main

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/units"
)

// cmdDecisions lists decisions, newest first.
func cmdDecisions(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	c := newCmd(stdout, stderr, "vaquero decisions [KEY] [--state open|answered|withdrawn] [--kind question|ruling|approval] [--ticket T] [--limit N]")
	state := c.fs.String("state", "", "open, answered or withdrawn")
	kind := c.fs.String("kind", "", "question, ruling or approval")
	ticket := c.fs.String("ticket", "", "only decisions about this ticket")
	limit := c.fs.Int("limit", 100, "at most this many")
	pos, err := c.parse(args, 0, 1)
	return c.connected(ctx, err, func() error {
		q := url.Values{"limit": {strconv.Itoa(*limit)}}
		if len(pos) == 1 {
			q.Set("project", pos[0])
		}
		for k, v := range map[string]string{"state": *state, "kind": *kind, "ticket": *ticket} {
			if v != "" {
				q.Set(k, v)
			}
		}
		var ds []api.Decision
		if err := c.call(ctx, "GET", "/api/v1/decisions?"+q.Encode(), nil, &ds); err != nil {
			return err
		}
		c.emit(ds, func() {
			rows := [][]string{{"ID", "KIND", "STATE", "TICKETS", "QUESTION", "ANSWER"}}
			for _, d := range ds {
				rows = append(rows, []string{d.ID, kindWords(d), d.State, strings.Join(d.Tickets, ","), units.Clip(d.Question, 60), units.Clip(d.Answer, 40)})
			}
			c.table(rows)
		})
		return nil
	})
}

// cmdDecision shows, asks, rules on, answers and withdraws decisions. Only
// the owner answers: without --agent the answer is the owner's own
// (direct); with --agent it is relayed by that agent and needs --words,
// the owner's answer quoted.
func cmdDecision(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	const usage = "vaquero decision show ID | ask KEY QUESTION [--ticket T]... [--option \"LABEL: TRADE-OFF\"]... [--approval plan|merge|release] [--recommend TEXT] [--why TEXT] [--run R]" +
		" | rule KEY QUESTION ANSWER [same flags] [--note TEXT] | answer ID [OPTION] [--text TEXT] [--note TEXT] [--words TEXT] | withdraw ID"
	c := newCmd(stdout, stderr, usage)
	var tickets, options stringList
	c.fs.Var(&tickets, "ticket", "a ticket it concerns (repeatable)")
	c.fs.Var(&options, "option", "an option, LABEL or LABEL: TRADE-OFF (repeatable)")
	approval := c.fs.String("approval", "", "ask for an approval of a plan, merge or release")
	recommend := c.fs.String("recommend", "", "the option you recommend")
	why := c.fs.String("why", "", "why it matters")
	run := c.fs.String("run", "", "the run it belongs to")
	note := c.fs.String("note", "", "a note on the answer")
	text := c.fs.String("text", "", "an answer in words, instead of an option")
	words := c.fs.String("words", "", "the owner's words, quoted, when an agent relays them")
	pos, err := c.parse(args, 2, 4)
	if err != nil {
		return c.finish(err)
	}
	askFlags := []string{"ticket", "option", "recommend", "why", "run"}
	verb := pos[0]
	shape := map[string]struct {
		min, max int
		flags    []string
	}{
		"show":     {2, 2, nil},
		"withdraw": {2, 2, nil},
		"ask":      {3, 3, append([]string{"approval"}, askFlags...)},
		"rule":     {4, 4, append([]string{"note"}, askFlags...)},
		"answer":   {2, 3, []string{"text", "note", "words"}},
	}
	s, ok := shape[verb]
	if !ok || len(pos) < s.min || len(pos) > s.max {
		return c.finish(usageError{usage: usage})
	}
	if err := c.only(verb, s.flags...); err != nil {
		return c.finish(err)
	}
	if verb == "answer" && (len(pos) == 3) == (*text != "") {
		return c.finish(usageError{usage: usage, hint: "answer takes an OPTION or --text, one of them"})
	}
	return c.connected(ctx, nil, func() error {
		var d api.Decision
		switch verb {
		case "show":
			if err := c.call(ctx, "GET", "/api/v1/decisions/"+url.PathEscape(pos[1]), nil, &d); err != nil {
				return err
			}
			c.emit(d, func() { printDecision(c.stdout, d) })
		case "withdraw":
			if err := c.call(ctx, "POST", "/api/v1/decisions/"+url.PathEscape(pos[1])+"/withdraw", nil, &d); err != nil {
				return err
			}
			c.emit(d, func() { fmt.Fprintf(c.stdout, "Withdrew %s.\n", d.ID) })
		case "ask", "rule":
			b := map[string]any{"project": pos[1], "question": pos[2], "kind": "question"}
			if *approval != "" {
				b["kind"], b["subject"] = "approval", *approval
			}
			if len(tickets) > 0 {
				b["tickets"] = []string(tickets)
			}
			opts := make([]api.DecisionOption, 0, len(options))
			for _, o := range options {
				label, trade, _ := strings.Cut(o, ":")
				opts = append(opts, api.DecisionOption{Label: strings.TrimSpace(label), Tradeoff: strings.TrimSpace(trade)})
			}
			if len(opts) > 0 {
				b["options"] = opts
			}
			for k, v := range map[string]string{"recommendation": *recommend, "why": *why, "run": *run} {
				if v != "" {
					b[k] = v
				}
			}
			if verb == "rule" {
				b["kind"] = "ruling"
				a := map[string]string{"text": pos[3]}
				for _, o := range opts {
					if strings.EqualFold(o.Label, strings.TrimSpace(pos[3])) {
						a = map[string]string{"option": o.Label}
					}
				}
				if *note != "" {
					a["note"] = *note
				}
				b["answer"] = a
			}
			if err := c.call(ctx, "POST", "/api/v1/decisions", b, &d); err != nil {
				return err
			}
			c.emit(d, func() { fmt.Fprintln(c.stdout, d.ID) })
		case "answer":
			b := map[string]string{}
			if len(pos) == 3 {
				b["option"] = pos[2]
			}
			for k, v := range map[string]string{"text": *text, "note": *note, "words": *words} {
				if v != "" {
					b[k] = v
				}
			}
			if err := c.call(ctx, "POST", "/api/v1/decisions/"+url.PathEscape(pos[1])+"/answer", b, &d); err != nil {
				return err
			}
			c.emit(d, func() {
				if d.Answered == "relayed" {
					fmt.Fprintf(c.stdout, "Relayed the owner's answer to %s: %s.\n", d.ID, strings.TrimRight(d.Answer, "."))
					return
				}
				fmt.Fprintf(c.stdout, "Answered %s: %s.\n", d.ID, strings.TrimRight(d.Answer, "."))
			})
		}
		return nil
	})
}

// kindWords is a decision's kind, with an approval's subject.
func kindWords(d api.Decision) string {
	if d.Subject != "" {
		return d.Kind + " (" + d.Subject + ")"
	}
	return d.Kind
}

// printDecision writes a decision for people: what was asked, the
// options, and the answer with who gave it and how.
func printDecision(w io.Writer, d api.Decision) {
	state := strings.ToUpper(d.State[:1]) + d.State[1:]
	fmt.Fprintf(w, "%s · %s · %s · %s\n", d.ID, strings.ToUpper(kindWords(d)[:1])+kindWords(d)[1:], state, d.Project)
	if len(d.Tickets) > 0 {
		fmt.Fprintf(w, "Tickets: %s\n", strings.Join(d.Tickets, ", "))
	}
	fmt.Fprintf(w, "Asked by %s · %s\n\n%s\n", d.AskedBy, d.AskedAt, d.Question)
	if len(d.Options) > 0 {
		fmt.Fprintln(w, "\nOptions:")
		for _, o := range d.Options {
			if o.Tradeoff != "" {
				fmt.Fprintf(w, "  - %s: %s\n", o.Label, o.Tradeoff)
			} else {
				fmt.Fprintf(w, "  - %s\n", o.Label)
			}
		}
	}
	if len(d.Asked) > 0 {
		// What an Approve covers, as asked.
		fmt.Fprintln(w, "\nApproving covers:")
		for _, a := range d.Asked {
			line := fmt.Sprintf("  %s at plan revision %d", a.Ticket, a.PlanRevision)
			if a.Parent != "" {
				line += ", under " + a.Parent
			}
			fmt.Fprintln(w, line)
			for _, c := range a.Children {
				fmt.Fprintf(w, "    %s  %s\n", c.Key, c.Title)
			}
		}
	}
	if d.Recommendation != "" {
		fmt.Fprintf(w, "\nRecommended: %s\n", d.Recommendation)
	}
	if d.Why != "" {
		fmt.Fprintf(w, "Why it matters: %s\n", d.Why)
	}
	if d.State == "answered" {
		how := ""
		switch d.Answered {
		case "direct":
			how = ", directly"
		case "relayed":
			how = ", relayed by " + d.RelayedBy
		}
		fmt.Fprintf(w, "\nAnswer: %s (%s%s · %s)\n", d.Answer, d.AnsweredBy, how, d.AnsweredAt)
		if d.Note != "" {
			fmt.Fprintf(w, "Note: %s\n", d.Note)
		}
		if d.Words != "" {
			fmt.Fprintf(w, "The owner's words: %s\n", d.Words)
		}
	}
	for _, a := range d.Approvals {
		fmt.Fprintf(w, "Approved %s at plan revision %d\n", a.Ticket, a.PlanRevision)
	}
}
