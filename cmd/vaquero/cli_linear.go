package main

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/client"
	"github.com/stazelabs/vaquero/internal/core"
)

// vaquero import linear FILE.csv [--project KEY] reads Linear's issue export
// (a CSV file) and makes each issue a ticket, keeping its team key and
// number (ENG-123 becomes ticket 123 of project ENG), so branch names and
// commit references naming it still resolve. It is an ordinary API client:
// the server makes every project, status and ticket through core, one
// event each, and tickets keep their numbers through core.ImportTicket
// (POST /api/v1/projects/{key}/tickets with "number").
//
// The columns are the ones Linear documents, and nothing else (ruling
// N-R4):
//
//   - https://linear.app/docs/exporting-data lists the export's columns
//     (linearColumns, in its order) and says the export carries no
//     attachment files.
//   - https://linear.app/docs/search gives an issue ID's form, the team
//     key and number ("LIN-123").
//   - https://linear.app/docs/issue-property names the priorities (No
//     priority, Low, Medium, High, Urgent) and the status categories
//     (Backlog, Unstarted, Started, Completed, Canceled, plus Duplicate and
//     Triage) and their default statuses (Backlog, Todo, In Progress, Done,
//     Canceled).
//   - Linear's own CSV importer, https://github.com/linear/linear/blob/master/packages/import/src/importers/linearCsv/LinearCsvImporter.ts,
//     reads the same file: labels separated by ", ", the priority names,
//     and a "'" Linear puts before a title or description that would read
//     as a spreadsheet formula. It reads the ID column as "Id" where the
//     documentation says "ID", so column names match whatever their case.
//
// The file says each issue's status by name, not by category. A name the
// project already has (whatever its case) is that status, and Linear's
// "Canceled" is the project's cancelled status. Any other name becomes a
// new status: Triage in backlog, Duplicate in cancelled, and the rest in
// the category the issues' Started, Completed and Canceled dates show
// (most of its issues decide; none of the dates is todo).
//
// Every row is read and checked, and every target project looked at,
// before anything is written: a project that has tickets or is archived
// is refused. A project is new unless --project names an empty one.
// Tickets are made in number order, since core refuses a number at or
// below one the project has given.
const linearUsage = "vaquero import linear FILE.csv [--project KEY]"

// linearColumns are the export's columns as linear.app/docs/exporting-data
// lists them.
var linearColumns = []string{
	"ID", "Team", "Title", "Description", "Status", "Estimate", "Priority", "Project ID", "Project", "Creator", "Assignee", "Labels",
	"Cycle Number", "Cycle Name", "Cycle Start", "Cycle End", "Created", "Updated", "Started", "Triaged", "Completed", "Canceled",
	"Archived", "Due Date", "Parent issue", "Initiatives", "Project Milestone ID", "Project Milestone", "SLA Status",
}

// linearRequired are the columns an import can't do without; the
// optional linearDates only decide a new status's category.
var (
	linearRequired = []string{"ID", "Team", "Title", "Description", "Status", "Priority", "Labels"}
	linearDates    = []string{"Started", "Completed", "Canceled"}
)

// linearPriorities are Linear's priorities as Vaquero's (the same scale:
// 0 none, 1 urgent … 4 low), by lower-case name.
var linearPriorities = map[string]int64{"": 0, "no priority": 0, "urgent": 1, "high": 2, "medium": 3, "low": 4}

// linearID is an issue ID: a team key and a number.
var linearID = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9]*)-0*([1-9][0-9]{0,17})$`)

// linearFormula is the quote Linear puts before text that would read as a
// formula, as Linear's importer strips it.
var linearFormula = regexp.MustCompile(`^'([+\-=@∑√∏<>＜＞≤≥＝≠±÷×])`)

// linearNotInCSV is what Linear's export doesn't carry.
var linearNotInCSV = []string{"comments", "relations", "attachments"}

// linearIssue is one row, checked.
type linearIssue struct {
	line        int
	team, key   string // the team's name, its key in upper case
	number      int64
	title, body string
	status      string
	priority    int64
	labels      []string
	category    string // from its dates: cancelled, done, in_progress or todo
}

// linearFile is a read export: its issues in number order, and the
// columns it has that aren't imported.
type linearFile struct {
	issues      []linearIssue
	notImported []string // documented columns the import doesn't read
	unknown     []string // columns Linear doesn't document
}

// linearResult is what an import did, as --json prints it.
type linearResult struct {
	Projects    []linearProject `json:"projects"`
	NotImported []string        `json:"not_imported_columns"`
	Unknown     []string        `json:"unknown_columns"`
	NotInCSV    []string        `json:"not_in_csv"`
}

// linearProject is one project an import filled.
type linearProject struct {
	Key         string       `json:"key"`
	Name        string       `json:"name"`
	Created     bool         `json:"created"`
	Tickets     int          `json:"tickets"`
	NewStatuses []api.Status `json:"new_statuses"`

	issues   []linearIssue
	statuses []api.Status
	team     string
}

func cmdImportLinear(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	c := newCmd(stdout, stderr, linearUsage)
	project := c.fs.String("project", "", "the project to import into, new or empty")
	pos, err := c.parse(args, 1, 1)
	if err != nil {
		return c.finish(err)
	}
	f, err := readLinear(pos[0])
	if err != nil {
		return c.finish(notImported{err})
	}
	targets, err := linearTargets(f, pos[0], strings.ToUpper(strings.TrimSpace(*project)), c.set("project"))
	if err != nil {
		return c.finish(err)
	}
	return c.connected(ctx, nil, func() error {
		for i := range targets {
			if err := c.checkLinearTarget(ctx, &targets[i]); err != nil {
				return notImported{err}
			}
		}
		res := linearResult{NotImported: f.notImported, Unknown: f.unknown, NotInCSV: linearNotInCSV}
		for i := range targets {
			err := c.fillLinear(ctx, &targets[i])
			res.Projects = append(res.Projects, targets[i])
			if err != nil {
				return err
			}
		}
		c.emit(res, func() { printLinear(c.stdout, res) })
		return nil
	})
}

// printLinear is the import's summary.
func printLinear(w io.Writer, res linearResult) {
	for _, p := range res.Projects {
		news := ""
		if p.Created {
			news = ", a new project"
		}
		fmt.Fprintf(w, "Imported %s into %s (%s)%s.\n", count(p.Tickets, "ticket"), p.Key, p.Name, news)
		if len(p.NewStatuses) > 0 {
			var ss []string
			for _, s := range p.NewStatuses {
				ss = append(ss, s.Name+" ("+categoryWords(s.Category)+")")
			}
			fmt.Fprintf(w, "New statuses in %s: %s.\n", p.Key, strings.Join(ss, ", "))
		}
	}
	fmt.Fprintln(w, "Comments, relations and attachments aren't in the CSV, so they weren't imported.")
	if len(res.NotImported) > 0 {
		fmt.Fprintf(w, "Columns not imported: %s.\n", strings.Join(res.NotImported, ", "))
	}
	if len(res.Unknown) > 0 {
		fmt.Fprintf(w, "Unknown columns, ignored: %s.\n", strings.Join(res.Unknown, ", "))
	}
}

// andList is "A", "A and B", "A, B and C".
func andList(xs []string) string {
	if len(xs) < 2 {
		return strings.Join(xs, "")
	}
	return strings.Join(xs[:len(xs)-1], ", ") + " and " + xs[len(xs)-1]
}

// linearText is a cell with Linear's formula quote removed.
func linearText(s string) string { return linearFormula.ReplaceAllString(s, "$1") }

// readLinear reads and checks an export. Each problem with a row names its
// line; at most ten are listed.
func readLinear(path string) (linearFile, error) {
	f := linearFile{notImported: []string{}, unknown: []string{}}
	name := filepath.Base(path)
	file, err := os.Open(path)
	if err != nil {
		return f, fmt.Errorf("can't read %s: %w", path, errUnwrapped(err))
	}
	defer file.Close()
	r := csv.NewReader(file)
	header, err := r.Read()
	if errors.Is(err, io.EOF) {
		return f, fmt.Errorf("%s is empty", name)
	}
	if err != nil {
		return f, fmt.Errorf("%s isn't a CSV file: %w", name, err)
	}
	col := map[string]int{} // documented column → its index
	for i, h := range header {
		h = strings.TrimSpace(strings.TrimPrefix(h, "\ufeff"))
		j := slices.IndexFunc(linearColumns, func(c string) bool { return strings.EqualFold(c, h) })
		switch {
		case j < 0 || hasCol(col, linearColumns[j]): // a repeat is unknown too
			f.unknown = append(f.unknown, h)
		default:
			col[linearColumns[j]] = i + 1 // 0 means absent
			if !slices.Contains(linearRequired, linearColumns[j]) && !slices.Contains(linearDates, linearColumns[j]) {
				f.notImported = append(f.notImported, h)
			}
		}
	}
	var missing []string
	for _, c := range linearRequired {
		if !hasCol(col, c) {
			missing = append(missing, c)
		}
	}
	switch len(missing) {
	case 0:
	case 1:
		return f, fmt.Errorf("%s is missing the column %s, which Linear's issue export has (https://linear.app/docs/exporting-data)", name, missing[0])
	default:
		return f, fmt.Errorf("%s is missing the columns %s, which Linear's issue export has (https://linear.app/docs/exporting-data)", name, andList(missing))
	}

	var problems []string
	bad := func(line int, format string, a ...any) {
		problems = append(problems, fmt.Sprintf("line %d: ", line)+fmt.Sprintf(format, a...))
	}
	seen := map[string]int{}
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if pe, ok := errors.AsType[*csv.ParseError](err); ok {
				return f, fmt.Errorf("%s isn't a CSV file Vaquero can read: line %d: %w", name, pe.Line, pe.Err)
			}
			return f, fmt.Errorf("can't read %s: %w", name, err)
		}
		line, _ := r.FieldPos(0)
		get := func(c string) string {
			if i := col[c]; i > 0 {
				return rec[i-1]
			}
			return ""
		}
		is := linearIssue{line: line}
		id := strings.TrimSpace(get("ID"))
		m := linearID.FindStringSubmatch(id)
		if m == nil {
			bad(line, "%q isn't an issue ID like ENG-123", id)
			continue
		}
		is.key = strings.ToUpper(m[1])
		is.number, _ = strconv.ParseInt(m[2], 10, 64)
		ref := fmt.Sprintf("%s-%d", is.key, is.number)
		if first, dup := seen[ref]; dup {
			bad(line, "%s is also on line %d", ref, first)
			continue
		}
		seen[ref] = line
		is.team = strings.TrimSpace(linearText(get("Team")))
		is.title = strings.TrimSpace(linearText(get("Title")))
		is.body = linearText(get("Description"))
		is.status = strings.TrimSpace(linearText(get("Status")))
		// The checks core makes, made here so a bad row stops the import
		// before anything is written.
		switch n := len([]rune(is.title)); {
		case n == 0:
			bad(line, "%s has no title", ref)
		case n > 200:
			bad(line, "%s's title is longer than 200 characters", ref)
		}
		if len([]rune(is.body)) > 100000 {
			bad(line, "%s's description is longer than 100,000 characters", ref)
		}
		if is.status == "" {
			bad(line, "%s has no status", ref)
		}
		p := strings.TrimSpace(get("Priority"))
		if v, ok := linearPriorities[strings.ToLower(p)]; ok {
			is.priority = v
		} else {
			bad(line, "unknown priority %q (Linear's are No priority, Urgent, High, Medium and Low)", p)
		}
		for l := range strings.SplitSeq(get("Labels"), ", ") {
			l = strings.TrimSpace(linearText(l))
			switch {
			case l == "":
			case len([]rune(l)) > 40:
				bad(line, "the label %s is longer than 40 characters, the most a tag has", l)
			default:
				is.labels = append(is.labels, l)
			}
		}
		switch {
		case strings.TrimSpace(get("Canceled")) != "":
			is.category = "cancelled"
		case strings.TrimSpace(get("Completed")) != "":
			is.category = "done"
		case strings.TrimSpace(get("Started")) != "":
			is.category = "in_progress"
		default:
			is.category = "todo"
		}
		f.issues = append(f.issues, is)
	}
	if len(problems) > 0 {
		if len(problems) > 10 {
			problems = append(problems[:10], fmt.Sprintf("and %d more", len(problems)-10))
		}
		return f, fmt.Errorf("%s can't be imported: %s", name, strings.Join(problems, "; "))
	}
	if len(f.issues) == 0 {
		return f, fmt.Errorf("%s has no issues", name)
	}
	slices.SortStableFunc(f.issues, func(a, b linearIssue) int {
		if c := strings.Compare(a.key, b.key); c != 0 {
			return c
		}
		return int(a.number - b.number)
	})
	return f, nil
}

func hasCol(col map[string]int, c string) bool { return col[c] > 0 }

// linearTargets groups the issues into one project per team key, or all
// into project when the flag is given (one team only).
func linearTargets(f linearFile, file, project string, given bool) ([]linearProject, error) {
	var teams []string
	for _, is := range f.issues {
		if !slices.Contains(teams, is.key) {
			teams = append(teams, is.key)
		}
	}
	if given && len(teams) > 1 {
		return nil, usageError{usage: linearUsage, hint: fmt.Sprintf("%s has issues from %d teams (%s); leave out --project to import each team into its own project",
			filepath.Base(file), len(teams), strings.Join(teams, ", "))}
	}
	var out []linearProject
	for _, team := range teams {
		key := team
		if given {
			key = project
		}
		if k, ok := core.ParseTicketKey(key + "-1"); !ok || k.Project != key {
			if given {
				return nil, usageError{usage: linearUsage, hint: "--project takes a project key: 2 to 10 letters or digits, starting with a letter"}
			}
			return nil, notImported{fmt.Errorf("%s isn't a project key here: give --project KEY, 2 to 10 letters or digits starting with a letter", team)}
		}
		lp := linearProject{Key: key, NewStatuses: []api.Status{}}
		for _, is := range f.issues {
			if is.key == team {
				lp.issues = append(lp.issues, is)
			}
		}
		lp.team = lp.issues[0].team
		if lp.team == "" {
			lp.team = key
		}
		out = append(out, lp)
	}
	return out, nil
}

// checkLinearTarget looks at the project an import fills: missing (it will
// be made), or there and empty, not archived.
func (c *cmd) checkLinearTarget(ctx context.Context, lp *linearProject) error {
	var p api.Project
	err := c.call(ctx, "GET", "/api/v1/projects/"+url.PathEscape(lp.Key), nil, &p)
	if ce, ok := errors.AsType[*client.Error](err); ok && ce.Status == http.StatusNotFound {
		lp.Created, lp.Name = true, lp.team
		return nil
	}
	if err != nil {
		return err
	}
	lp.Name = p.Name
	if p.Archived {
		return fmt.Errorf("%s is archived; restore it, or give --project KEY for a new or empty project", lp.Key)
	}
	if err := c.call(ctx, "GET", statusesPath(lp.Key), nil, &lp.statuses); err != nil {
		return err
	}
	for _, s := range lp.statuses {
		if s.TicketCount > 0 {
			return fmt.Errorf("%s already has tickets; give --project KEY for a new or empty project", lp.Key)
		}
	}
	return nil
}

// fillLinear makes the project when it is new, its new statuses, then the
// tickets in number order. A failure part way names the ticket it stopped
// at; the tickets before it stay.
func (c *cmd) fillLinear(ctx context.Context, lp *linearProject) error {
	if lp.Created {
		var p api.Project
		if err := c.call(ctx, "POST", "/api/v1/projects", map[string]string{"key": lp.Key, "name": lp.Name}, &p); err != nil {
			return notImported{fmt.Errorf("can't make project %s: %w", lp.Key, err)}
		}
		if err := c.call(ctx, "GET", statusesPath(lp.Key), nil, &lp.statuses); err != nil {
			return err
		}
	}
	names, news := linearStatuses(lp.statuses, lp.issues)
	for _, s := range news {
		var made api.Status
		if err := c.call(ctx, "POST", statusesPath(lp.Key), map[string]string{"name": s.Name, "category": s.Category}, &made); err != nil {
			return fmt.Errorf("can't add status %s to %s: %w. No tickets were imported into %s", s.Name, lp.Key, err, lp.Key)
		}
		lp.NewStatuses = append(lp.NewStatuses, made)
	}
	for _, is := range lp.issues {
		body := map[string]any{
			"number": is.number, "title": is.title, "body": is.body,
			"status": names[strings.ToLower(is.status)], "priority": is.priority, "tags": append([]string{}, is.labels...),
		}
		var tk api.Ticket
		if err := c.call(ctx, "POST", "/api/v1/projects/"+url.PathEscape(lp.Key)+"/tickets", body, &tk); err != nil {
			return fmt.Errorf("stopped at %s-%d (line %d): %w. %s has the %s before it", is.key, is.number, is.line, err, lp.Key, count(lp.Tickets, "ticket"))
		}
		lp.Tickets++
	}
	return nil
}

// linearStatuses maps each of the issues' status names (lower case) to a
// status of the project, and lists the statuses to add.
func linearStatuses(have []api.Status, issues []linearIssue) (map[string]string, []api.Status) {
	votes := map[string]map[string]int{}
	for _, is := range issues {
		k := strings.ToLower(is.status)
		if votes[k] == nil {
			votes[k] = map[string]int{}
		}
		votes[k][is.category]++
	}
	names := map[string]string{}
	var news []api.Status
	for _, is := range issues {
		k := strings.ToLower(is.status)
		if _, done := names[k]; done {
			continue
		}
		if i := slices.IndexFunc(have, func(s api.Status) bool { return strings.EqualFold(s.Name, is.status) }); i >= 0 {
			names[k] = have[i].Name
			continue
		}
		if k == "canceled" {
			if i := slices.IndexFunc(have, func(s api.Status) bool { return s.Category == "cancelled" }); i >= 0 {
				names[k] = have[i].Name
				continue
			}
		}
		cat := ""
		switch k {
		case "triage":
			cat = "backlog"
		case "duplicate", "canceled":
			cat = "cancelled"
		default:
			best := -1
			for _, c := range []string{"cancelled", "done", "in_progress", "todo"} {
				if n := votes[k][c]; n > best {
					cat, best = c, n
				}
			}
		}
		names[k] = is.status
		news = append(news, api.Status{Name: is.status, Category: cat})
	}
	return names, news
}
