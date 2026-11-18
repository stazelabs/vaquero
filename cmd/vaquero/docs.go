package main

import (
	"context"
	"fmt"
	"io"

	"github.com/stazelabs/vaquero/internal/help"
)

// cmdDocs is `vaquero docs [TOPIC]`: the help articles, listed or printed as
// markdown. It needs no server.
func cmdDocs(_ context.Context, stdout, stderr io.Writer, args []string) int {
	switch len(args) {
	case 0:
		for _, s := range help.Sections() {
			fmt.Fprintln(stdout, s.Title)
			c := &cmd{stdout: stdout}
			var rows [][]string
			for _, a := range s.Articles {
				rows = append(rows, []string{"  " + a.Slug, a.Title})
			}
			c.table(rows)
			fmt.Fprintln(stdout)
		}
		fmt.Fprintln(stdout, "Print one with: vaquero docs TOPIC")
		return exitOK
	case 1:
		a, ok := help.Get(args[0])
		if !ok {
			fmt.Fprintf(stderr, "no help topic called %q; run vaquero docs to list them\n", args[0])
			return exitUsage
		}
		fmt.Fprint(stdout, a.Markdown)
		return exitOK
	}
	fmt.Fprintln(stderr, "usage: vaquero docs [TOPIC]")
	return exitUsage
}
