package main

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/client"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/idgen"
)

type apiTicket = api.Ticket

// cliCommands are the API-backed commands, by first argument.
var cliCommands = map[string]func(ctx context.Context, stdout, stderr io.Writer, args []string) int{
	"projects": cmdProjects, "project": cmdProject,
	"tickets": cmdTickets, "ticket": cmdTicket,
	"comment": cmdComment, "plan": cmdPlan, "decisions": cmdDecisions, "decision": cmdDecision, "link": cmdLink, "unlink": cmdUnlink, "docs": cmdDocs,
	"search": cmdSearch, "activity": cmdActivity,
	"token": cmdToken, "profile": cmdProfile,
}

// canon is a ticket key in its canonical form, for paths.
func canon(key string) (string, error) {
	k, ok := core.ParseTicketKey(key)
	if !ok {
		return "", fmt.Errorf("%s isn't a ticket key", key)
	}
	return k.String(), nil
}

// connected runs fn after connecting, and finishes with its error.
func (c *cmd) connected(ctx context.Context, err error, fn func() error) int {
	if err == nil {
		err = c.connect(ctx)
	}
	if err == nil {
		err = fn()
	}
	return c.finish(err)
}

func cmdProjects(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	c := newCmd(stdout, stderr, "vaquero projects")
	_, err := c.parse(args, 0, 0)
	return c.connected(ctx, err, func() error {
		var ps []api.Project
		if err := c.call(ctx, "GET", "/api/v1/projects", nil, &ps); err != nil {
			return err
		}
		c.emit(ps, func() {
			rows := [][]string{{"KEY", "NAME", "OPEN"}}
			for _, p := range ps {
				name := p.Name
				if p.Archived {
					name += " (archived)"
				}
				rows = append(rows, []string{p.Key, name, strconv.FormatInt(p.OpenCount, 10)})
			}
			c.table(rows)
		})
		return nil
	})
}

func cmdProject(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	const usage = "vaquero project create KEY NAME [--description TEXT] | show KEY | edit KEY [--name NAME] [--description TEXT] [--workflow on|off] [--repo PATH] [--main-branch NAME] [--remote NAME] [--fetch-minutes N] | key KEY NEWKEY | icon KEY --monogram|--glyph NAME|--image FILE|--remove-image | link KEY depends-on|relates OTHER | unlink KEY ID|OTHER"
	c := newCmd(stdout, stderr, usage)
	name := c.fs.String("name", "", "new name")
	desc := c.fs.String("description", "", "description (- reads standard input)")
	workflow := c.fs.String("workflow", "", "on or off: the agent workflow")
	repo := c.fs.String("repo", "", "the project's git repository: an absolute path, empty to clear (owner only)")
	mainBranch := c.fs.String("main-branch", "", "the repository's main branch (owner only)")
	remote := c.fs.String("remote", "", "the repository's remote, empty for origin (owner only)")
	fetchMinutes := c.fs.Int64("fetch-minutes", 0, "fetch the repository every N minutes, 0 for never")
	c.fs.Bool("monogram", false, "icon: the project's monogram")
	glyph := c.fs.String("glyph", "", "icon: a glyph from the list")
	image := c.fs.String("image", "", "icon: a PNG, JPEG or WebP image file")
	c.fs.Bool("remove-image", false, "icon: remove the uploaded image (an icon showing it becomes the monogram)")
	pos, err := c.parse(args, 2, 4)
	if err != nil {
		return c.finish(err)
	}
	if len(pos) == 4 && pos[0] != "link" {
		return c.finish(usageError{usage: usage})
	}
	var p api.Project
	printProject := func() {
		c.emit(p, func() {
			fmt.Fprintf(c.stdout, "%s  %s\n", p.Key, p.Name)
			if p.Workflow {
				fmt.Fprintln(c.stdout, "Workflow: on")
			}
			for _, l := range p.Links {
				fmt.Fprintf(c.stdout, "%s %s  %s  %s\n", projectLinkWords(l), l.Key, l.Name, l.ID)
			}
			if r := p.Repo; r != nil {
				fmt.Fprintf(c.stdout, "Repository: %s (main branch %s)\n", r.Path, r.MainBranch)
				if r.RemoteURL != "" {
					fmt.Fprintf(c.stdout, "Remote: %s\n", r.RemoteURL)
				}
				if r.FetchMinutes > 0 {
					fmt.Fprintf(c.stdout, "Fetch every %d minutes\n", r.FetchMinutes)
				}
				if !r.Available {
					fmt.Fprintln(c.stdout, "The repository can't be read now.")
				}
			}
			fmt.Fprintln(c.stdout, p.Description)
		})
	}
	if c.set("workflow") && (pos[0] != "edit" || *workflow != "on" && *workflow != "off") {
		return c.finish(usageError{usage: usage, hint: "--workflow is on or off, for project edit"})
	}
	for _, f := range []string{"repo", "main-branch", "remote", "fetch-minutes"} {
		if c.set(f) && pos[0] != "edit" {
			return c.finish(usageError{usage: usage, hint: "--" + f + " is for project edit"})
		}
	}
	icons := 0
	for _, f := range []string{"monogram", "glyph", "image", "remove-image"} {
		if c.set(f) {
			icons++
		}
	}
	if icons > 0 && pos[0] != "icon" || pos[0] == "icon" && icons != 1 {
		return c.finish(usageError{usage: usage, hint: "project icon takes one of --monogram, --glyph NAME, --image FILE and --remove-image"})
	}
	switch pos[0] {
	case "create":
		if len(pos) != 3 {
			return c.finish(usageError{usage: usage})
		}
		return c.connected(ctx, nil, func() error {
			d, err := text(*desc)
			if err != nil {
				return err
			}
			if err := c.call(ctx, "POST", "/api/v1/projects", map[string]string{"key": pos[1], "name": pos[2], "description": d}, &p); err != nil {
				return err
			}
			printProject()
			return nil
		})
	case "show":
		if len(pos) != 2 {
			return c.finish(usageError{usage: usage})
		}
		return c.connected(ctx, nil, func() error {
			if err := c.call(ctx, "GET", "/api/v1/projects/"+url.PathEscape(pos[1]), nil, &p); err != nil {
				return err
			}
			printProject()
			return nil
		})
	case "edit":
		if len(pos) != 2 {
			return c.finish(usageError{usage: usage})
		}
		body := map[string]any{}
		if c.set("name") {
			body["name"] = *name
		}
		if c.set("workflow") {
			body["workflow"] = *workflow == "on"
		}
		for flag, key := range map[string]string{"repo": "repo_path", "main-branch": "main_branch", "remote": "remote"} {
			if c.set(flag) {
				body[key] = map[string]string{"repo": *repo, "main-branch": *mainBranch, "remote": *remote}[flag]
			}
		}
		if c.set("fetch-minutes") {
			body["fetch_minutes"] = *fetchMinutes
		}
		return c.connected(ctx, nil, func() error {
			if c.set("description") {
				d, err := text(*desc)
				if err != nil {
					return err
				}
				body["description"] = d
			}
			if err := c.call(ctx, "PATCH", "/api/v1/projects/"+url.PathEscape(pos[1]), body, &p); err != nil {
				return err
			}
			printProject()
			return nil
		})
	case "key":
		if len(pos) != 3 {
			return c.finish(usageError{usage: usage})
		}
		return c.connected(ctx, nil, func() error {
			if err := c.call(ctx, "POST", "/api/v1/projects/"+url.PathEscape(pos[1])+"/key", map[string]string{"key": pos[2]}, &p); err != nil {
				return err
			}
			printProject()
			return nil
		})
	case "link":
		if len(pos) != 4 || pos[2] != "depends-on" && pos[2] != "relates" {
			return c.finish(usageError{usage: usage})
		}
		return c.connected(ctx, nil, func() error {
			var l api.ProjectLink
			if err := c.call(ctx, "POST", "/api/v1/projects/"+url.PathEscape(pos[1])+"/links", map[string]string{"kind": pos[2], "project": pos[3]}, &l); err != nil {
				return err
			}
			c.emit(l, func() {
				fmt.Fprintf(c.stdout, "%s %s %s.\n", strings.ToUpper(pos[1]), strings.ToLower(projectLinkWords(l)), l.Key)
			})
			return nil
		})
	case "unlink":
		if len(pos) != 3 {
			return c.finish(usageError{usage: usage})
		}
		return c.connected(ctx, nil, func() error { return unlinkProject(ctx, c, pos[1], pos[2]) })
	case "icon":
		if len(pos) != 2 {
			return c.finish(usageError{usage: usage})
		}
		return c.connected(ctx, nil, func() error {
			path := "/api/v1/projects/" + url.PathEscape(pos[1])
			var err error
			switch {
			case c.set("image"):
				err = uploadIcon(ctx, c, path+"/icon", *image, &p)
			case c.set("remove-image"):
				if err = c.call(ctx, "DELETE", path+"/icon", nil, &p); err != nil {
					return err
				}
				c.emit(p, func() { fmt.Fprintf(c.stdout, "%s icon: %s (no image stored)\n", p.Key, iconWords(p)) })
				return nil
			case c.set("glyph"):
				err = c.call(ctx, "PATCH", path, map[string]any{"icon": map[string]string{"kind": "glyph", "glyph": *glyph}}, &p)
			default:
				err = c.call(ctx, "PATCH", path, map[string]any{"icon": map[string]string{"kind": "monogram"}}, &p)
			}
			if err != nil {
				return err
			}
			c.emit(p, func() { fmt.Fprintf(c.stdout, "%s icon: %s\n", p.Key, iconWords(p)) })
			return nil
		})
	}
	return c.finish(usageError{usage: usage})
}

// projectLinkWords is how a project's link reads from its side: "Depends
// on", "Used by" or "Relates to".
func projectLinkWords(l api.ProjectLink) string {
	switch {
	case l.Kind == "depends on" && l.Direction == "out":
		return "Depends on"
	case l.Kind == "depends on":
		return "Used by"
	}
	return "Relates to"
}

// unlinkProject removes key's link named by what: a link id, or another
// project's key (every link between the two).
func unlinkProject(ctx context.Context, c *cmd, key, what string) error {
	key = strings.ToUpper(key)
	path := "/api/v1/projects/" + url.PathEscape(key)
	var p api.Project
	if err := c.call(ctx, "GET", path, nil, &p); err != nil {
		return err
	}
	byID := idgen.Valid(strings.ToUpper(what))
	other, removed := strings.ToUpper(what), 0
	for _, l := range p.Links {
		if byID && l.ID == strings.ToUpper(what) || !byID && l.Key == other {
			if err := c.call(ctx, "DELETE", path+"/links/"+url.PathEscape(l.ID), nil, nil); err != nil {
				return err
			}
			other = l.Key
			removed++
		}
	}
	if removed == 0 && byID {
		// Not one of key's: the server says so in its words.
		return c.call(ctx, "DELETE", path+"/links/"+url.PathEscape(what), nil, nil)
	}
	if removed == 0 {
		return fmt.Errorf("%s isn't linked to %s", key, other)
	}
	if !c.jsonOut {
		noun := "link"
		if removed > 1 {
			noun = "links"
		}
		fmt.Fprintf(c.stdout, "Removed %d %s between %s and %s.\n", removed, noun, key, other)
	}
	return nil
}

// errIconTooLarge is the refusal of an icon image over core.MaxIcon, in
// the server's words.
const errIconTooLarge = "Use an image of 256 KB or less."

// uploadIcon sends file as the project icon image at path, after checking
// it is a file of at most core.MaxIcon bytes.
func uploadIcon(ctx context.Context, c *cmd, path, file string, out *api.Project) error {
	st, err := os.Stat(file)
	if err != nil {
		return fmt.Errorf("can't read %s: %w", file, errUnwrapped(err))
	}
	if st.IsDir() {
		return fmt.Errorf("%s is a directory", file)
	}
	if st.Size() > core.MaxIcon {
		return fmt.Errorf("%s: %s", filepath.Base(file), errIconTooLarge)
	}
	fh, err := os.Open(file)
	if err != nil {
		return fmt.Errorf("can't read %s: %w", file, errUnwrapped(err))
	}
	defer fh.Close()
	return c.client.UploadPut(ctx, path, filepath.Base(file), fh, out)
}

// iconWords says what p's icon is: "monogram VA", "glyph rocket", "image".
func iconWords(p api.Project) string {
	switch {
	case p.Icon == nil:
		return "monogram"
	case p.Icon.Kind == core.IconKindGlyph:
		return "glyph " + p.Icon.Glyph
	case p.Icon.Kind == core.IconKindImage:
		return "image"
	}
	return "monogram " + core.Monogram(p.Name, p.Key)
}

func cmdTickets(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	c := newCmd(stdout, stderr, "vaquero tickets KEY [--status S] [--tag T] [--priority P] [--milestone NAME] [--mine] [--agents] [--all] [--limit N]")
	status := c.fs.String("status", "", "status name")
	tag := c.fs.String("tag", "", "tag")
	prio := c.fs.String("priority", "", "priority")
	milestone := c.fs.String("milestone", "", "milestone name, or none")
	mine := c.fs.Bool("mine", false, "assigned to you")
	agents := c.fs.Bool("agents", false, "assigned to agents")
	all := c.fs.Bool("all", false, "include done and cancelled")
	limit := c.fs.Int("limit", 100, "at most this many")
	pos, err := c.parse(args, 1, 1)
	return c.connected(ctx, err, func() error {
		q := url.Values{"limit": {strconv.Itoa(*limit)}}
		if *status != "" {
			q.Set("status", *status)
		} else if !*all {
			q.Set("open", "1")
		}
		if *tag != "" {
			q.Set("tag", *tag)
		}
		if *prio != "" {
			p, err := parsePriority(*prio)
			if err != nil {
				return err
			}
			q.Set("priority", strconv.FormatInt(p, 10))
		}
		if *milestone != "" {
			q.Set("milestone", *milestone)
		}
		if *mine {
			q.Set("mine", "1")
		}
		if *agents {
			q.Set("agents", "1")
		}
		var ts []api.TicketSummary
		if err := c.call(ctx, "GET", "/api/v1/projects/"+url.PathEscape(pos[0])+"/tickets?"+q.Encode(), nil, &ts); err != nil {
			return err
		}
		c.emit(ts, func() {
			rows := [][]string{{"KEY", "STATUS", "PRIORITY", "TITLE", "TAGS", "ASSIGNEE"}}
			for _, t := range ts {
				rows = append(rows, []string{t.Key, t.Status, priorityWords[t.Priority], t.Title, strings.Join(t.Tags, ","), t.Assignee})
			}
			c.table(rows)
		})
		return nil
	})
}

// printTicket writes the human view of a ticket.
func printTicket(w io.Writer, t apiTicket) {
	fmt.Fprintf(w, "%s  %s\n", t.Key, t.Title)
	fmt.Fprintf(w, "Status: %s · Priority: %s · Version: %d\n", t.Status, priorityWords[t.Priority], t.Version)
	if t.Assignee != "" {
		fmt.Fprintf(w, "Assignee: %s\n", t.Assignee)
	}
	if t.ClaimedBy != "" {
		fmt.Fprintf(w, "Claimed by %s until %s\n", t.ClaimedBy, untilWords(t.ClaimExpiresAt))
	}
	if t.Milestone != "" {
		fmt.Fprintf(w, "Milestone: %s\n", t.Milestone)
	}
	if len(t.FormerKeys) > 0 {
		fmt.Fprintf(w, "Formerly %s\n", strings.Join(t.FormerKeys, ", "))
	}
	switch {
	case t.Tier != "" && t.Model != "":
		fmt.Fprintf(w, "Tier: %s · Model: %s\n", t.Tier, t.Model)
	case t.Tier != "":
		fmt.Fprintf(w, "Tier: %s\n", t.Tier)
	case t.Model != "":
		fmt.Fprintf(w, "Model: %s\n", t.Model)
	}
	if r := t.Readiness; r != nil { // the agent workflow is on
		if r.Ready {
			fmt.Fprintln(w, "Readiness: ready")
		} else {
			fmt.Fprintf(w, "Readiness: not ready. %s\n", strings.Join(r.Reasons, " "))
		}
	}
	if t.Parent != "" {
		fmt.Fprintf(w, "Parent: %s\n", t.Parent)
	}
	if len(t.Tags) > 0 {
		fmt.Fprintf(w, "Tags: %s\n", strings.Join(t.Tags, ", "))
	}
	if t.Body != "" {
		fmt.Fprintf(w, "\n%s\n", t.Body)
	}
	if len(t.SubTickets) > 0 {
		fmt.Fprintln(w, "\nSub-tickets:")
		for _, s := range t.SubTickets {
			fmt.Fprintf(w, "  %s  %s  (%s)\n", s.Key, s.Title, s.Status)
		}
	}
	if len(t.Links) > 0 {
		fmt.Fprintln(w, "\nLinks:")
		for _, l := range t.Links {
			fmt.Fprintf(w, "  %s %s  %s\n", linkWords(l), l.Key, l.Title)
		}
	}
	if len(t.Comments) > 0 {
		fmt.Fprintln(w, "\nComments:")
		for _, c := range t.Comments {
			fmt.Fprintf(w, "  %s, %s:\n    %s\n", c.Author, c.CreatedAt, strings.ReplaceAll(c.Body, "\n", "\n    "))
		}
	}
}

func linkWords(l api.Link) string {
	out := map[string]string{"blocks": "blocks", "relates": "relates to", "duplicates": "duplicates"}
	in := map[string]string{"blocks": "blocked by", "relates": "relates to", "duplicates": "duplicated by"}
	if l.Direction == "in" {
		return in[l.Kind]
	}
	return out[l.Kind]
}

func cmdTicket(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	const usage = "vaquero ticket show TICKET | new KEY TITLE [fields] | edit TICKET [fields] [--add-tag T] [--remove-tag T] [--version N] | move TICKET STATUS | move-to TICKET KEY [--version N]"
	c := newCmd(stdout, stderr, usage)
	title := c.fs.String("title", "", "title")
	body := c.fs.String("body", "", "description (- reads standard input)")
	status := c.fs.String("status", "", "status name")
	prio := c.fs.String("priority", "", "none, urgent, high, medium or low")
	parent := c.fs.String("parent", "", "parent ticket key")
	milestone := c.fs.String("milestone", "", "milestone name")
	assignee := c.fs.String("assignee", "", "assignee name")
	tier := c.fs.String("tier", "", "design, light, standard or heavy; empty clears it")
	model := c.fs.String("model", "", "exact model id; empty for the tier map's choice")
	var tags, addTags, removeTags stringList
	c.fs.Var(&tags, "tag", "tag (repeat for more)")
	c.fs.Var(&addTags, "add-tag", "add a tag (repeatable)")
	c.fs.Var(&removeTags, "remove-tag", "remove a tag (repeatable)")
	version := c.fs.Int64("version", 0, "refuse the edit if the ticket changed since this version")
	pos, err := c.parse(args, 2, 3)
	if err != nil {
		return c.finish(err)
	}
	if c.set("version") && *version < 1 {
		return c.finish(usageError{usage: usage, hint: "--version is a ticket version, 1 or more"})
	}
	// fields gathers the flags that were given into a request body.
	fields := func() (map[string]any, error) {
		b := map[string]any{}
		for name, v := range map[string]*string{"title": title, "status": status, "parent": parent, "milestone": milestone, "assignee": assignee, "tier": tier, "model": model} {
			if c.set(name) {
				b[name] = *v
			}
		}
		if c.set("body") {
			t, err := text(*body)
			if err != nil {
				return nil, err
			}
			b["body"] = t
		}
		if c.set("priority") {
			p, err := parsePriority(*prio)
			if err != nil {
				return nil, err
			}
			b["priority"] = p
		}
		if c.set("tag") {
			b["tags"] = []string(tags)
		}
		if len(addTags) > 0 {
			b["add_tags"] = []string(addTags)
		}
		if len(removeTags) > 0 {
			b["remove_tags"] = []string(removeTags)
		}
		return b, nil
	}
	var t apiTicket
	show := func() { c.emit(t, func() { printTicket(c.stdout, t) }) }
	switch pos[0] {
	case "show":
		if len(pos) != 2 {
			return c.finish(usageError{usage: usage})
		}
		return c.connected(ctx, nil, func() error {
			key, err := canon(pos[1])
			if err != nil {
				return err
			}
			if err := c.call(ctx, "GET", "/api/v1/tickets/"+key, nil, &t); err != nil {
				return err
			}
			show()
			return nil
		})
	case "new":
		if len(pos) != 3 {
			return c.finish(usageError{usage: usage})
		}
		for _, f := range []string{"version", "remove-tag"} {
			if c.set(f) {
				return c.finish(usageError{usage: usage, hint: "--" + f + " is for ticket edit"})
			}
		}
		return c.connected(ctx, nil, func() error {
			b, err := fields()
			if err != nil {
				return err
			}
			b["title"] = pos[2]
			if err := c.call(ctx, "POST", "/api/v1/projects/"+url.PathEscape(pos[1])+"/tickets", b, &t); err != nil {
				return err
			}
			c.emit(t, func() { fmt.Fprintln(c.stdout, t.Key) })
			return nil
		})
	case "edit":
		if len(pos) != 2 {
			return c.finish(usageError{usage: usage})
		}
		return c.connected(ctx, nil, func() error {
			key, err := canon(pos[1])
			if err != nil {
				return err
			}
			b, err := fields()
			if err != nil {
				return err
			}
			if *version > 0 {
				b["version"] = *version
			}
			if err := c.call(ctx, "PATCH", "/api/v1/tickets/"+key, b, &t); err != nil {
				return err
			}
			show()
			return nil
		})
	case "move":
		if len(pos) != 3 {
			return c.finish(usageError{usage: usage})
		}
		return c.connected(ctx, nil, func() error {
			key, err := canon(pos[1])
			if err != nil {
				return err
			}
			if err := c.call(ctx, "POST", "/api/v1/tickets/"+key+"/move", map[string]string{"status": pos[2]}, &t); err != nil {
				return err
			}
			show()
			return nil
		})
	case "move-to":
		if len(pos) != 3 {
			return c.finish(usageError{usage: usage})
		}
		return c.connected(ctx, nil, func() error {
			key, err := canon(pos[1])
			if err != nil {
				return err
			}
			// The key it has now, which an old key typed here leads to.
			var before apiTicket
			if err := c.call(ctx, "GET", "/api/v1/tickets/"+key, nil, &before); err != nil {
				return err
			}
			b := map[string]any{"project": pos[2]}
			if *version > 0 {
				b["version"] = *version
			}
			if err := c.call(ctx, "POST", "/api/v1/tickets/"+key+"/move-to", b, &t); err != nil {
				return err
			}
			c.emit(t, func() { fmt.Fprintf(c.stdout, "%s is now %s.\n", before.Key, t.Key) })
			return nil
		})
	}
	return c.finish(usageError{usage: usage})
}

func cmdComment(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	c := newCmd(stdout, stderr, "vaquero comment TICKET TEXT  (TEXT - reads standard input)")
	pos, err := c.parse(args, 2, 2)
	return c.connected(ctx, err, func() error {
		key, err := canon(pos[0])
		if err != nil {
			return err
		}
		body, err := text(pos[1])
		if err != nil {
			return err
		}
		var cm api.Comment
		if err := c.call(ctx, "POST", "/api/v1/tickets/"+key+"/comments", map[string]string{"body": body}, &cm); err != nil {
			return err
		}
		c.emit(cm, func() { fmt.Fprintf(c.stdout, "Commented on %s.\n", key) })
		return nil
	})
}

func cmdLink(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	const usage = "vaquero link TICKET blocks|relates|duplicates OTHER"
	c := newCmd(stdout, stderr, usage)
	pos, err := c.parse(args, 3, 3)
	if err == nil && pos[1] != "blocks" && pos[1] != "relates" && pos[1] != "duplicates" {
		err = usageError{usage: usage}
	}
	return c.connected(ctx, err, func() error {
		key, err := canon(pos[0])
		if err != nil {
			return err
		}
		var l api.Link
		if err := c.call(ctx, "POST", "/api/v1/tickets/"+key+"/links", map[string]string{"kind": pos[1], "to": pos[2]}, &l); err != nil {
			return err
		}
		c.emit(l, func() { fmt.Fprintf(c.stdout, "%s %s %s.\n", key, linkWords(l), l.Key) })
		return nil
	})
}

func cmdUnlink(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	c := newCmd(stdout, stderr, "vaquero unlink TICKET OTHER")
	pos, err := c.parse(args, 2, 2)
	return c.connected(ctx, err, func() error {
		key, err := canon(pos[0])
		if err != nil {
			return err
		}
		other, err := canon(pos[1])
		if err != nil {
			return err
		}
		var t apiTicket
		if err := c.call(ctx, "GET", "/api/v1/tickets/"+key, nil, &t); err != nil {
			return err
		}
		removed := 0
		for _, l := range t.Links {
			if l.Key == other {
				if err := c.call(ctx, "DELETE", "/api/v1/links/"+url.PathEscape(l.ID), nil, nil); err != nil {
					return err
				}
				removed++
			}
		}
		if removed == 0 {
			return fmt.Errorf("%s isn't linked to %s", key, other)
		}
		if !c.jsonOut {
			noun := "link"
			if removed > 1 {
				noun = "links"
			}
			fmt.Fprintf(c.stdout, "Removed %d %s between %s and %s.\n", removed, noun, key, other)
		}
		return nil
	})
}

func cmdSearch(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	c := newCmd(stdout, stderr, "vaquero search WORDS...")
	pos, err := c.parse(args, 1, -1)
	return c.connected(ctx, err, func() error {
		var hits []api.SearchHit
		if err := c.call(ctx, "GET", "/api/v1/search?q="+url.QueryEscape(strings.Join(pos, " ")), nil, &hits); err != nil {
			return err
		}
		c.emit(hits, func() {
			if len(hits) == 0 {
				fmt.Fprintln(c.stdout, "No tickets match.")
				return
			}
			rows := [][]string{{"KEY", "STATUS", "TITLE", "MATCH"}}
			for _, h := range hits {
				rows = append(rows, []string{h.Key, h.Status, h.Title, h.Snippet})
			}
			c.table(rows)
		})
		return nil
	})
}

func cmdActivity(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	c := newCmd(stdout, stderr, "vaquero activity [--after SEQ] [--limit N]")
	after := c.fs.Int64("after", 0, "only events after this sequence number")
	limit := c.fs.Int("limit", 20, "at most this many (the newest, unless --after)")
	_, err := c.parse(args, 0, 0)
	return c.connected(ctx, err, func() error {
		if *limit < 1 {
			return usageError{usage: c.usage}
		}
		q := url.Values{"limit": {strconv.Itoa(*limit)}}
		if *after > 0 {
			q.Set("after", strconv.FormatInt(*after, 10))
		} else {
			q = url.Values{"newest": {strconv.Itoa(*limit)}}
		}
		var evs []api.Event
		if err := c.call(ctx, "GET", "/api/v1/events?"+q.Encode(), nil, &evs); err != nil {
			return err
		}
		c.emit(evs, func() {
			rows := [][]string{{"SEQ", "WHEN", "WHO", "WHAT"}}
			for _, e := range evs {
				rows = append(rows, []string{strconv.FormatInt(e.Seq, 10), e.At, e.Actor, e.Text})
			}
			c.table(rows)
		})
		return nil
	})
}

func cmdToken(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	const usage = "vaquero token create NAME | list | revoke PREFIX"
	c := newCmd(stdout, stderr, usage)
	pos, err := c.parse(args, 1, 2)
	if err != nil {
		return c.finish(err)
	}
	switch {
	case pos[0] == "create" && len(pos) == 2:
		return c.connected(ctx, nil, func() error {
			var t api.Token
			if err := c.call(ctx, "POST", "/api/v1/tokens", map[string]string{"name": pos[1]}, &t); err != nil {
				return err
			}
			c.emit(t, func() {
				fmt.Fprintln(c.stdout, t.Token)
				fmt.Fprintln(c.stderr, "Copy this token now. It won't be shown again.")
			})
			return nil
		})
	case pos[0] == "list" && len(pos) == 1:
		return c.connected(ctx, nil, func() error {
			var ts []api.Token
			if err := c.call(ctx, "GET", "/api/v1/tokens", nil, &ts); err != nil {
				return err
			}
			c.emit(ts, func() {
				rows := [][]string{{"PREFIX", "NAME", "ACTS AS", "LAST USED", "STATE"}}
				for _, t := range ts {
					state := "live"
					if t.Revoked {
						state = "revoked"
					}
					rows = append(rows, []string{t.Prefix, t.Name, t.Actor, t.LastUsedAt, state})
				}
				c.table(rows)
			})
			return nil
		})
	case pos[0] == "revoke" && len(pos) == 2:
		return c.connected(ctx, nil, func() error {
			if err := c.call(ctx, "POST", "/api/v1/tokens/"+url.PathEscape(pos[1])+"/revoke", nil, nil); err != nil {
				return err
			}
			if !c.jsonOut {
				fmt.Fprintf(c.stdout, "Revoked %s.\n", pos[1])
			}
			return nil
		})
	}
	return c.finish(usageError{usage: usage})
}

// cmdProfile manages saved profiles; it never talks to a server.
func cmdProfile(_ context.Context, stdout, stderr io.Writer, args []string) int {
	const usage = "vaquero profile add NAME URL TOKEN|- | list | use NAME | remove NAME"
	c := newCmd(stdout, stderr, usage)
	pos, err := c.parse(args, 1, 4)
	if err != nil {
		return c.finish(err)
	}
	cfg, err := client.Load()
	if err != nil {
		return c.finish(err)
	}
	find := func(name string) int {
		for i, p := range cfg.Profiles {
			if p.Name == name {
				return i
			}
		}
		return -1
	}
	switch {
	case pos[0] == "add" && len(pos) == 4:
		if pos[1] == "local" {
			return c.finish(fmt.Errorf("local is built in; pick another name"))
		}
		addr := strings.TrimRight(pos[2], "/")
		if u, err := url.Parse(addr); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(addr, "?#") {
			return c.finish(fmt.Errorf("%s isn't a server address; give one like https://vaquero.example.com", pos[2]))
		}
		// "-" reads the token from standard input, keeping it out of the
		// shell's history and the process list.
		token, err := text(pos[3])
		if err != nil {
			return c.finish(err)
		}
		if token = strings.TrimSpace(token); token == "" {
			return c.finish(message("No token on standard input."))
		}
		p := client.Profile{Name: pos[1], URL: addr, Token: token}
		if i := find(p.Name); i >= 0 {
			cfg.Profiles[i] = p
		} else {
			cfg.Profiles = append(cfg.Profiles, p)
		}
		return c.finish(cfg.Save())
	case pos[0] == "list" && len(pos) == 1:
		current := cfg.Current
		if current == "" {
			current = "local"
		}
		rows := [][]string{{"", "NAME", "URL"}, {mark(current == "local"), "local", "this machine"}}
		for _, p := range cfg.Profiles {
			rows = append(rows, []string{mark(current == p.Name), p.Name, p.URL})
		}
		c.table(rows)
		return exitOK
	case pos[0] == "use" && len(pos) == 2:
		if pos[1] != "local" && find(pos[1]) < 0 {
			return c.finish(fmt.Errorf("no profile called %s", pos[1]))
		}
		cfg.Current = pos[1]
		if pos[1] == "local" {
			cfg.Current = ""
		}
		return c.finish(cfg.Save())
	case pos[0] == "remove" && len(pos) == 2:
		i := find(pos[1])
		if i < 0 {
			return c.finish(fmt.Errorf("no profile called %s", pos[1]))
		}
		cfg.Profiles = append(cfg.Profiles[:i], cfg.Profiles[i+1:]...)
		if cfg.Current == pos[1] {
			cfg.Current = ""
		}
		return c.finish(cfg.Save())
	}
	return c.finish(usageError{usage: usage})
}

func mark(on bool) string {
	if on {
		return "*"
	}
	return ""
}
