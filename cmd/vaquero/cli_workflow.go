package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/stazelabs/vaquero/internal/workflow"
)

func init() { cliCommands["workflow"] = cmdWorkflow }

// workflowFlags is every prompt argument's name, once, in List's order:
// each is a flag of vaquero workflow.
func workflowFlags() []string {
	var names []string
	for _, p := range workflow.List() {
		for _, a := range p.Args {
			if !slices.Contains(names, a.Name) {
				names = append(names, a.Name)
			}
		}
	}
	return names
}

// argUsage is how an argument is written on the command line: "--kind
// qa|hygiene|continuity|docs", or "--ticket TICKET".
func argUsage(a workflow.Arg) string {
	v := strings.ToUpper(a.Name)
	if a.Name == "project" {
		v = "KEY"
	}
	if len(a.Values) > 0 {
		v = strings.Join(a.Values, "|")
	}
	s := "--" + a.Name + " " + v
	if !a.Required {
		s = "[" + s + "]"
	}
	return s
}

// cmdWorkflow is `vaquero workflow`: the workflow prompts listed, one
// printed with its arguments (the text MCP's prompts/get answers, byte for
// byte), or an audit pack template printed as written. It needs no
// server and touches no network or data directory.
func cmdWorkflow(_ context.Context, stdout, stderr io.Writer, args []string) int {
	const usage = "vaquero workflow [NAME --ARG VALUE...] | pack KIND"
	c := newCmd(stdout, stderr, usage)
	flags := workflowFlags()
	values := map[string]*string{}
	for _, n := range flags {
		values[n] = c.fs.String(n, "", "a workflow prompt's argument")
	}
	name, rest := splitVerb(args, flags...)
	switch name {
	case "":
		if _, err := c.parse(rest, 0, 0); err != nil {
			return c.finish(err)
		}
		if c.only("the list") != nil {
			return c.finish(usageError{usage: usage, hint: "give a prompt's NAME before its arguments"})
		}
		return listWorkflow(c)
	case "pack":
		pos, err := c.parse(rest, 1, 1)
		if err == nil {
			err = c.only("pack")
		}
		if err != nil {
			return c.finish(err)
		}
		b, ok := workflow.Pack(strings.ToLower(pos[0]))
		if !ok {
			fmt.Fprintf(stderr, "Give a pack kind: %s.\n", workflow.Allowed(workflow.Packs()))
			return exitUsage
		}
		fmt.Fprint(stdout, b)
		return exitOK
	}
	p, ok := workflow.Get(name)
	if !ok {
		fmt.Fprintf(stderr, "no workflow prompt called %q; run vaquero workflow to list them\n", name)
		return exitUsage
	}
	if _, err := c.parse(rest, 0, 0); err != nil {
		return c.finish(err)
	}
	var takes []string
	for _, a := range p.Args {
		takes = append(takes, a.Name)
	}
	if err := c.only(name, takes...); err != nil {
		return c.finish(err)
	}
	given := map[string]string{}
	for _, n := range takes {
		given[n] = *values[n]
	}
	text, err := workflow.Render(p, given)
	if e, ok := errors.AsType[*workflow.ArgError](err); ok {
		fmt.Fprintln(stderr, e.Say("--"))
		return exitUsage
	}
	if err != nil {
		return c.finish(err)
	}
	fmt.Fprint(stdout, text)
	return exitOK
}

// workflowJSON is a prompt as `vaquero workflow --json` lists it.
type workflowJSON struct {
	Name        string    `json:"name"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Args        []argJSON `json:"args"`
}

type argJSON struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Required    bool     `json:"required"`
	Values      []string `json:"values,omitempty"`
}

func listWorkflow(c *cmd) int {
	ps := workflow.List()
	out := make([]workflowJSON, 0, len(ps))
	for _, p := range ps {
		w := workflowJSON{Name: p.Name, Title: p.Title, Description: p.Description, Args: []argJSON{}}
		for _, a := range p.Args {
			w.Args = append(w.Args, argJSON{Name: a.Name, Description: a.Description, Required: a.Required, Values: a.Values})
		}
		out = append(out, w)
	}
	c.emit(out, func() {
		rows := [][]string{{"NAME", "TITLE", "ARGUMENTS"}}
		for _, p := range ps {
			var args []string
			for _, a := range p.Args {
				args = append(args, argUsage(a))
			}
			rows = append(rows, []string{p.Name, p.Title, strings.Join(args, " ")})
		}
		c.table(rows)
		fmt.Fprintln(c.stdout)
		fmt.Fprintln(c.stdout, "Print one with: vaquero workflow NAME and its arguments")
		fmt.Fprintf(c.stdout, "Print an audit pack template with: vaquero workflow pack KIND (%s)\n", workflow.Allowed(workflow.Packs()))
	})
	return exitOK
}
