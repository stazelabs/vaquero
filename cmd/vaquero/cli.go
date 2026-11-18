package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/stazelabs/vaquero/internal/client"
	"github.com/stazelabs/vaquero/internal/version"
)

// Exit codes for CLI commands.
const (
	exitOK       = 0
	exitFail     = 1
	exitUsage    = 2
	exitConflict = 3 // the ticket changed since it was read: re-read and retry
)

// stdin is where "-" arguments are read from; tests replace it.
var stdin io.Reader

// startWait is how long the CLI waits for a local server it started.
const startWait = 5 * time.Second

// usageError is a command used wrongly: exit 2 with its usage line.
type usageError struct{ usage string }

func (e usageError) Error() string { return "usage: " + e.usage }

// cmd is one CLI invocation: its output streams, flags and client.
type cmd struct {
	stdout, stderr io.Writer
	fs             *flag.FlagSet
	jsonOut        bool
	profile        string
	usage          string
	client         *client.Client
}

// newCmd starts a command whose usage line is usage; every command takes
// --profile and --json.
func newCmd(stdout, stderr io.Writer, usage string) *cmd {
	c := &cmd{stdout: stdout, stderr: stderr, usage: usage, fs: flag.NewFlagSet(usage, flag.ContinueOnError)}
	c.fs.SetOutput(io.Discard)
	c.fs.StringVar(&c.profile, "profile", "", "server profile")
	c.fs.BoolVar(&c.jsonOut, "json", false, "print JSON")
	return c
}

// parse reads flags anywhere among the arguments and returns the
// positional ones, which must number between min and max (-1: any).
func (c *cmd) parse(args []string, min, max int) ([]string, error) {
	var pos []string
	for {
		if err := c.fs.Parse(args); err != nil {
			return nil, usageError{c.usage}
		}
		args = c.fs.Args()
		if len(args) == 0 {
			break
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
	if len(pos) < min || max >= 0 && len(pos) > max {
		return nil, usageError{c.usage}
	}
	return pos, nil
}

// set reports whether the flag called name was given.
func (c *cmd) set(name string) bool {
	found := false
	c.fs.Visit(func(f *flag.Flag) { found = found || f.Name == name })
	return found
}

// connect builds the client for the chosen profile. For the local server
// it starts vaquero when it isn't running and reads the token it wrote.
func (c *cmd) connect(ctx context.Context) error {
	p, local, err := client.Resolve(c.profile, os.Getenv)
	if err != nil {
		return err
	}
	if local {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		if err := client.EnsureLocal(ctx, exe, p.URL, p.DataDir, startWait); err != nil {
			return err
		}
		if p.Token == "" {
			p.Token = client.LocalToken(p.DataDir)
		}
	}
	c.client = &client.Client{BaseURL: p.URL, Token: p.Token, Agent: os.Getenv("VAQUERO_AGENT"), Version: version.Version}
	return nil
}

// call sends one API request; after it, an outdated CLI says so.
func (c *cmd) call(ctx context.Context, method, path string, body, out any) error {
	err := c.client.Do(ctx, method, path, body, out)
	if min, need := c.client.UpgradeNeeded(); need {
		fmt.Fprintf(c.stderr, "This server needs vaquero %s or newer; this is %s. Upgrade vaquero.\n", min, version.Version)
	}
	return err
}

// emit prints v as JSON with --json, else calls human.
func (c *cmd) emit(v any, human func()) {
	if c.jsonOut {
		b, _ := json.MarshalIndent(v, "", "  ")
		fmt.Fprintln(c.stdout, string(b))
		return
	}
	human()
}

// table writes rows aligned in columns.
func (c *cmd) table(rows [][]string) {
	tw := tabwriter.NewWriter(c.stdout, 0, 0, 2, ' ', 0)
	for _, r := range rows {
		fmt.Fprintln(tw, strings.Join(r, "\t"))
	}
	tw.Flush()
}

// finish turns a command's error into its exit code, printing the message.
// A conflict prints the ticket as it is now and exits 3.
func (c *cmd) finish(err error) int {
	if err == nil {
		return exitOK
	}
	if u, ok := errors.AsType[usageError](err); ok {
		fmt.Fprintln(c.stderr, u.Error())
		return exitUsage
	}
	var apiErr *client.Error
	if errors.As(err, &apiErr) && apiErr.Status == 409 && len(apiErr.Current) > 0 {
		fmt.Fprintln(c.stderr, apiErr.Message)
		if c.jsonOut {
			fmt.Fprintln(c.stdout, string(apiErr.Current))
		} else {
			var t apiTicket
			if json.Unmarshal(apiErr.Current, &t) == nil {
				printTicket(c.stdout, t)
			}
		}
		return exitConflict
	}
	fmt.Fprintln(c.stderr, err.Error())
	return exitFail
}

// text reads "-" as standard input, anything else as itself.
func text(arg string) (string, error) {
	if arg != "-" {
		return arg, nil
	}
	r := stdin
	if r == nil {
		r = os.Stdin
	}
	b, err := io.ReadAll(r)
	return strings.TrimRight(string(b), "\n"), err
}

// stringList is a repeatable flag.
type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

// priorities maps the words the CLI accepts to the API's numbers.
var priorities = map[string]int64{"none": 0, "urgent": 1, "high": 2, "medium": 3, "low": 4,
	"0": 0, "1": 1, "2": 2, "3": 3, "4": 4}

var priorityWords = []string{"No priority", "Urgent", "High", "Medium", "Low"}

func parsePriority(s string) (int64, error) {
	p, ok := priorities[strings.ToLower(strings.TrimSpace(s))]
	if !ok {
		return 0, fmt.Errorf("priority is one of none, urgent, high, medium or low")
	}
	return p, nil
}
