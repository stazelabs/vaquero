// Command backlog-import moves docs/BACKLOG.md into a running Vaquero, once.
// Kept in the repo as the record of how the backlog became tickets.
//
// Without flags it prints the plan: the VAQ project (its description is the
// backlog's Direction section), a milestone for each of Phases 4 to 7, and a
// todo ticket for every open line, tagged by section. With -apply it first
// exports the instance (the undo point), then creates everything through the
// vaquero CLI, and the milestones through the server's settings form (the
// CLI has no milestone command). It refuses when the project already exists.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/stazelabs/vaquero/internal/config"
	"github.com/stazelabs/vaquero/internal/timefmt"
)

func main() {
	file := flag.String("file", "docs/BACKLOG.md", "the backlog to import")
	apply := flag.Bool("apply", false, "create the project, milestones and tickets (prints the plan otherwise)")
	bin := flag.String("bin", "vaquero", "the vaquero binary to run")
	server := flag.String("url", "http://127.0.0.1:7878", "the local server, for the milestone form")
	key := flag.String("key", "VAQ", "project key")
	flag.Parse()
	md, err := os.ReadFile(*file)
	if err != nil {
		fail(err)
	}
	p, err := Parse(string(md))
	if err != nil {
		fail(err)
	}
	printPlan(p, *key)
	if !*apply {
		fmt.Println("\nNothing was created. Run again with -apply to create it.")
		return
	}
	if err := run(context.Background(), p, *bin, *server, *key); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "backlog-import:", err)
	os.Exit(1)
}

func printPlan(p Plan, key string) {
	fmt.Printf("Project %s \"Vaquero\" (description: the Direction section, %d lines)\n\n", key, strings.Count(p.ProjectDescription, "\n")+1)
	fmt.Printf("Milestones (%d):\n", len(p.Milestones))
	for _, m := range p.Milestones {
		fmt.Printf("  %s\n", m.Name)
	}
	byTag := map[string]int{}
	var tags []string
	for _, t := range p.Tickets {
		if byTag[t.Tag] == 0 {
			tags = append(tags, t.Tag)
		}
		byTag[t.Tag]++
	}
	fmt.Printf("\nTickets (%d), all todo:\n", len(p.Tickets))
	for _, tag := range tags {
		fmt.Printf("\n  [%s] %d\n", tag, byTag[tag])
		for _, t := range p.Tickets {
			if t.Tag != tag {
				continue
			}
			ms := ""
			if t.Milestone != "" {
				ms = "  → " + t.Milestone
			}
			fmt.Printf("    %s%s\n", t.Title, ms)
		}
	}
}

func run(ctx context.Context, p Plan, bin, server, key string) error {
	if _, err := vq(ctx, bin, "", "project", "show", key); err == nil {
		return fmt.Errorf("project %s already exists; nothing was created", key)
	}
	dataDir, err := dataDir()
	if err != nil {
		return err
	}
	undo := filepath.Join(dataDir, "backups", "before-backlog-"+timefmt.FileStamp(time.Now())+".tar.gz")
	if err := os.MkdirAll(filepath.Dir(undo), 0o700); err != nil {
		return err
	}
	if out, err := vq(ctx, bin, "", "export", undo); err != nil {
		return fmt.Errorf("export before importing: %v: %s", err, out)
	}
	fmt.Println("\nUndo point:", undo)
	if out, err := vq(ctx, bin, p.ProjectDescription, "project", "create", "--description", "-", "--", key, "Vaquero"); err != nil {
		return fmt.Errorf("create project: %v: %s", err, out)
	}
	for _, m := range p.Milestones {
		if err := createMilestone(ctx, server, key, m); err != nil {
			return fmt.Errorf("milestone %q: %w", m.Name, err)
		}
	}
	for i, t := range p.Tickets {
		args := []string{"ticket", "new", "--body", "-", "--tag", t.Tag, "--json"}
		if t.Milestone != "" {
			args = append(args, "--milestone", t.Milestone)
		}
		args = append(args, "--", key, t.Title)
		out, err := vq(ctx, bin, t.Body, args...)
		if err != nil {
			return fmt.Errorf("ticket %d of %d (%q): %v: %s\nThe undo point is %s", i+1, len(p.Tickets), t.Title, err, out, undo)
		}
		var got struct {
			Key string `json:"key"`
		}
		_ = json.Unmarshal([]byte(out), &got)
		fmt.Printf("  %s  %s\n", got.Key, t.Title)
	}
	fmt.Printf("\nCreated %s with %d milestones and %d tickets.\n", key, len(p.Milestones), len(p.Tickets))
	return nil
}

// vq runs the CLI with stdin, returning its output.
func vq(ctx context.Context, bin, stdin string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// createMilestone posts the project settings form, as the page does; a
// success redirects back to settings.
func createMilestone(ctx context.Context, server, key string, m Milestone) error {
	form := url.Values{"name": {m.Name}, "description": {m.Description}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server+"/p/"+key+"/milestones", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusSeeOther && res.StatusCode != http.StatusFound {
		return fmt.Errorf("the server answered %s", res.Status)
	}
	return nil
}

func dataDir() (string, error) {
	if d := os.Getenv("VQ_DATA_DIR"); d != "" {
		return d, nil
	}
	d, err := config.DefaultDataDir(runtime.GOOS, os.Getenv)
	if err != nil {
		return "", errors.New("can't find the data directory; set VQ_DATA_DIR")
	}
	return d, nil
}
