package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/stazelabs/vaquero/internal/client"
	"github.com/stazelabs/vaquero/internal/lockfile"
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

// startWait is how long the CLI waits for a local server it started. The
// first boot of a new version snapshots and migrates the database before
// it answers, which can take a while on a big one; a server that stops
// while starting is reported at once (EnsureLocal), not after this.
const startWait = 60 * time.Second

// usageError is a command used wrongly: exit 2 with its usage line and,
// when there is one, a hint.
type usageError struct{ usage, hint string }

func (e usageError) Error() string {
	if e.hint != "" {
		return e.hint + "\nusage: " + e.usage
	}
	return "usage: " + e.usage
}

// dashHint explains how to pass text that starts with "-".
const dashHint = "to pass text starting with -, put -- before it, or use - to read it from standard input"

// cmd is one CLI invocation: its output streams, flags and client.
type cmd struct {
	stdout, stderr io.Writer
	fs             *flag.FlagSet
	jsonOut        bool
	server         string
	agent          string
	usage          string
	client         *client.Client
	// restartLocal starts the local server again and returns its token;
	// set by connect for the local profile (long-running commands use it).
	restartLocal func(ctx context.Context) (string, error)
	// upgradeTold: the upgrade message was printed; it shows once a run.
	upgradeTold bool
	// outsideTold: the note that the server runs outside brew services
	// was printed; it too shows once a run.
	outsideTold bool
	// runningTold: "Vaquero is running at ..." was printed; once a run.
	runningTold bool
	// printCurrent prints a conflict's current object; nil prints it as a
	// ticket.
	printCurrent func(current json.RawMessage)
}

// newCmd starts a command whose usage line is usage; every command takes
// --server, --agent and --json.
func newCmd(stdout, stderr io.Writer, usage string) *cmd {
	c := &cmd{stdout: stdout, stderr: stderr, usage: usage, fs: flag.NewFlagSet(usage, flag.ContinueOnError)}
	c.fs.SetOutput(io.Discard)
	c.fs.StringVar(&c.server, "server", "", "the server to talk to (see vaquero server list)")
	c.fs.StringVar(&c.agent, "agent", "", "act as this agent (local mode; default $VAQUERO_AGENT)")
	c.fs.BoolVar(&c.jsonOut, "json", false, "print JSON")
	return c
}

// parse reads flags anywhere among the arguments before a "--" and
// returns the positional ones (everything after "--" is positional), which
// must number between min and max (-1: any).
func (c *cmd) parse(args []string, min, max int) ([]string, error) {
	var rest []string
	if i := slices.Index(args, "--"); i >= 0 {
		args, rest = args[:i], args[i+1:]
	}
	var pos []string
	for {
		if err := c.fs.Parse(args); err != nil {
			return nil, usageError{usage: c.usage, hint: dashHint}
		}
		args = c.fs.Args()
		if len(args) == 0 {
			break
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
	pos = append(pos, rest...)
	if len(pos) < min || max >= 0 && len(pos) > max {
		return nil, usageError{usage: c.usage}
	}
	return pos, nil
}

// splitVerb finds a noun's sub-verb after leading flags (VAQ-192: flags may
// come before or after, so the verb isn't always args[0]). It skips flags,
// and the value of --server, --agent and any flag in valued that isn't
// written --flag=value, and returns the verb with args minus the verb; no
// verb (the arguments end, or "--" comes first) is "" and args as given.
func splitVerb(args []string, valued ...string) (string, []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			return "", args
		case !strings.HasPrefix(a, "-") || a == "-":
			return a, slices.Concat(args[:i:i], args[i+1:])
		case strings.Contains(a, "="):
		default:
			name := strings.TrimLeft(a, "-")
			if name == "server" || name == "agent" || slices.Contains(valued, name) {
				i++
			}
		}
	}
	return "", args
}

// set reports whether the flag called name was given.
func (c *cmd) set(name string) bool {
	found := false
	c.fs.Visit(func(f *flag.Flag) { found = found || f.Name == name })
	return found
}

// only refuses, as a usage error, any flag given beyond the common ones
// (--server, --agent, --json) and those the verb takes.
func (c *cmd) only(verb string, takes ...string) error {
	var extra string
	c.fs.Visit(func(f *flag.Flag) {
		switch {
		case extra != "", f.Name == "server", f.Name == "agent", f.Name == "json", slices.Contains(takes, f.Name):
		default:
			extra = f.Name
		}
	})
	if extra != "" {
		return usageError{usage: c.usage, hint: verb + " doesn't take --" + extra}
	}
	return nil
}

// connect builds the client for the chosen server. For the local server
// it starts vaquero when it isn't running and reads the token it wrote.
func (c *cmd) connect(ctx context.Context) error {
	p, local, err := client.Resolve(c.server, os.Getenv)
	if err != nil {
		return err
	}
	if local {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		if err := c.startLocal(ctx, exe, p); err != nil {
			return err
		}
		// Read the token now: the server may have just written it, or
		// written a new one after the old was revoked.
		p.Token = client.LocalToken(p.DataDir)
		if err := c.replaceStale(ctx, exe, p); err != nil {
			return err
		}
		p.Token = client.LocalToken(p.DataDir)
		c.restartLocal = func(ctx context.Context) (string, error) {
			if err := c.startLocal(ctx, exe, p); err != nil {
				return "", err
			}
			return client.LocalToken(p.DataDir), nil
		}
	}
	agent := c.agent
	if agent == "" {
		agent = os.Getenv("VAQUERO_AGENT")
	}
	c.client = &client.Client{BaseURL: p.URL, Token: p.Token, Agent: agent, Version: version.Version}
	return nil
}

// brewRestartWait is how long to wait for brew services to bring the
// server back: launchd may hold a restart for up to 10 seconds. Tests
// shorten it.
var brewRestartWait = 20 * time.Second

// replaceStale restarts the local server when it runs an older release than
// this CLI: after `brew upgrade vaquero`, the running server is still the
// old binary. A newer server, or a dev build on either side, is left alone,
// so two binaries never take turns restarting each other.
func (c *cmd) replaceStale(ctx context.Context, exe string, p client.Profile) error {
	own := version.Version
	if client.IsDev(own) {
		return nil
	}
	// Only a server this data directory's lock names is ours to replace; one
	// from another data directory (same address) is left alone, and the pid
	// pins the stop to the server the probe is about to be answered by.
	pid, held, _ := lockfile.Holder(p.DataDir)
	if !held {
		return nil
	}
	probe := &client.Client{BaseURL: p.URL, Token: p.Token, Version: own}
	if err := probe.Do(ctx, "GET", "/api/v1/me", nil, nil); err != nil {
		return nil // the command's own request reports any problem
	}
	running := probe.ServerVersion()
	if client.IsDev(running) || !client.Older(running, own) {
		return nil
	}
	fmt.Fprintf(c.stderr, "vaquero: restarting the local server to run %s (it was running %s)\n", own, running)
	if stopped, err := stopLocal(ctx, p.DataDir, stopWait, pid); err != nil || !stopped {
		return err // not stopped: another command already replaced it
	}
	return c.startLocal(ctx, exe, p)
}

// errBrewGone is the answer when the brew service is loaded but its server
// never answers.
var errBrewGone = message("The brew service didn't come back; run brew services restart vaquero.")

// startLocal makes sure the local server answers, starting it when nothing
// listens. Under brew services the service manager starts it: a server
// started here would hold the port and the data directory, and the
// service's own would fail and be retried every few seconds for as long as
// it ran. So wait for the service, and when it never comes, say so and
// start nothing. With the service file there but nothing loaded (brew
// services stop --keep), no service will start it: start it here, and say
// once how to hand it back.
func (c *cmd) startLocal(ctx context.Context, exe string, p client.Profile) error {
	// A server that answers needs nothing more, and no question to the
	// service manager (after replaceStale's stop nothing answers yet, so
	// the restart still asks).
	if client.WaitUp(ctx, p.URL, 0) == nil {
		return nil
	}
	label, state := brewServiceState(p.DataDir)
	if state == brewRunning {
		// The command waited for the service to bring the server up, so
		// it says where it is, as for a server it started itself.
		if client.WaitUp(ctx, p.URL, brewRestartWait) == nil {
			c.tellRunning(p.URL)
			return nil
		}
		if label, state = brewServiceState(p.DataDir); state == brewRunning {
			return errBrewGone
		}
	}
	if state != brewStopped {
		return c.ensureLocal(ctx, exe, p)
	}
	if client.WaitUp(ctx, p.URL, 0) == nil {
		return nil
	}
	if err := c.ensureLocal(ctx, exe, p); err != nil {
		return err
	}
	if !c.outsideTold {
		c.outsideTold = true
		fmt.Fprintf(c.stderr, "vaquero: the brew service (%s) isn't loaded, so this server runs outside brew services; to hand it back, run vaquero stop, then brew services start vaquero\n", label)
	}
	return nil
}

// ensureLocal is client.EnsureLocal, telling where the server is when this
// command started it.
func (c *cmd) ensureLocal(ctx context.Context, exe string, p client.Profile) error {
	started, err := client.EnsureLocal(ctx, exe, p.URL, p.DataDir, startWait)
	if started && err == nil {
		c.tellRunning(p.URL)
	}
	return err
}

// tellRunning says, once a run and on standard error, where the server a
// command just started (or waited for) answers.
func (c *cmd) tellRunning(url string) {
	if c.runningTold {
		return
	}
	c.runningTold = true
	fmt.Fprintf(c.stderr, "Vaquero is running at %s.\n", strings.TrimRight(url, "/"))
}

// call sends one API request; after it, an outdated CLI says so.
func (c *cmd) call(ctx context.Context, method, path string, body, out any) error {
	err := c.client.Do(ctx, method, path, body, out)
	c.tellUpgrade()
	c.tellWarnings()
	return err
}

// tellWarnings prints each warning the last answers carried, such as
// someone else's claim on the ticket just changed, on stderr.
func (c *cmd) tellWarnings() {
	for _, w := range c.client.Warnings() {
		fmt.Fprintln(c.stderr, "vaquero: warning: "+w)
	}
}

// tellUpgrade says, once a run, that this CLI is older than the server's
// minimum (dev builds are never told: client.UpgradeNeeded).
func (c *cmd) tellUpgrade() {
	if min, need := c.client.UpgradeNeeded(); need && !c.upgradeTold {
		c.upgradeTold = true
		fmt.Fprintf(c.stderr, "This server needs vaquero %s or newer; this is %s. Upgrade vaquero.\n", min, version.Version)
	}
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
// A conflict prints the ticket (or the plan) as it is now and exits 3.
func (c *cmd) finish(err error) int {
	if err == nil {
		return exitOK
	}
	if u, ok := errors.AsType[usageError](err); ok {
		fmt.Fprintln(c.stderr, u.Error())
		return exitUsage
	}
	var apiErr *client.Error
	if errors.As(err, &apiErr) && apiErr.Code == "conflict" {
		fmt.Fprintln(c.stderr, apiErr.Message)
		switch {
		case len(apiErr.Current) == 0:
		case c.jsonOut:
			fmt.Fprintln(c.stdout, string(apiErr.Current))
		case c.printCurrent != nil:
			c.printCurrent(apiErr.Current)
		default:
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
