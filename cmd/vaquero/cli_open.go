package main

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/stazelabs/vaquero/internal/core"
)

// openBrowser shows url in the default browser. Tests replace it so none
// opens a real one.
var openBrowser = func(url string) error {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, name, url).Run()
}

var projectKey = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]{1,9}$`)

// cmdOpen is `vaquero open [KEY|TICKET]`: the web UI in the browser, at
// the projects page, a project's Focus page or a ticket's page. It starts
// the local server like any command. When no browser can be opened it
// prints the address instead.
func cmdOpen(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	const usage = "vaquero open [KEY|TICKET]"
	c := newCmd(stdout, stderr, usage)
	pos, err := c.parse(args, 0, 1)
	path := "/"
	if err == nil && len(pos) == 1 {
		if k, ok := core.ParseTicketKey(pos[0]); ok {
			path = "/t/" + k.String()
		} else if projectKey.MatchString(pos[0]) {
			path = "/p/" + strings.ToUpper(pos[0])
		} else {
			err = usageError{usage: usage, hint: pos[0] + " isn't a project key or a ticket key"}
		}
	}
	if err == nil {
		err = c.only("open")
	}
	return c.connected(ctx, err, func() error {
		url := strings.TrimRight(c.client.BaseURL, "/") + path
		if openBrowser(url) != nil {
			fmt.Fprintf(c.stdout, "Open %s in your browser.\n", url)
		}
		return nil
	})
}
