package templates

import "strings"

// DiffLine is one line of a line diff: Op is ' ' (unchanged), '-'
// (removed), '+' (added) or '…' (unchanged lines left out).
type DiffLine struct {
	Op   rune
	Text string
}

// diffContext is how many unchanged lines show around each change.
const diffContext = 3

// diffCells bounds the LCS table (the lines left after trimming the common
// start and end, one side times the other). Past it the middle reads as
// every old line removed, then every new line added.
const diffCells = 1 << 20

// LineDiff compares two texts line by line: the longest common subsequence
// of their lines, with long unchanged runs folded to diffContext lines on
// each side of a change. It is plain Go, no library, and bounded by
// diffCells.
func LineDiff(a, b string) []DiffLine {
	x, y := splitLines(a), splitLines(b)
	pre := 0
	for pre < len(x) && pre < len(y) && x[pre] == y[pre] {
		pre++
	}
	suf := 0
	for suf < len(x)-pre && suf < len(y)-pre && x[len(x)-1-suf] == y[len(y)-1-suf] {
		suf++
	}
	var out []DiffLine
	for _, l := range x[:pre] {
		out = append(out, DiffLine{' ', l})
	}
	out = append(out, lcsDiff(x[pre:len(x)-suf], y[pre:len(y)-suf])...)
	for _, l := range x[len(x)-suf:] {
		out = append(out, DiffLine{' ', l})
	}
	return foldUnchanged(out)
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	ls := strings.Split(s, "\n")
	for i, l := range ls {
		ls[i] = strings.TrimSuffix(l, "\r")
	}
	return ls
}

func lcsDiff(x, y []string) []DiffLine {
	n, m := len(x), len(y)
	var out []DiffLine
	if n*m > diffCells {
		for _, l := range x {
			out = append(out, DiffLine{'-', l})
		}
		for _, l := range y {
			out = append(out, DiffLine{'+', l})
		}
		return out
	}
	// dp[i*(m+1)+j] is the LCS length of x[i:] and y[j:].
	dp := make([]int32, (n+1)*(m+1))
	at := func(i, j int) int32 { return dp[i*(m+1)+j] }
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if x[i] == y[j] {
				dp[i*(m+1)+j] = at(i+1, j+1) + 1
			} else {
				dp[i*(m+1)+j] = max(at(i+1, j), at(i, j+1))
			}
		}
	}
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case x[i] == y[j]:
			out = append(out, DiffLine{' ', x[i]})
			i, j = i+1, j+1
		case at(i+1, j) >= at(i, j+1):
			out = append(out, DiffLine{'-', x[i]})
			i++
		default:
			out = append(out, DiffLine{'+', y[j]})
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, DiffLine{'-', x[i]})
	}
	for ; j < m; j++ {
		out = append(out, DiffLine{'+', y[j]})
	}
	return out
}

// foldUnchanged keeps diffContext unchanged lines on each side of a change
// and replaces the rest of each unchanged run with one '…' line.
func foldUnchanged(ls []DiffLine) []DiffLine {
	var out []DiffLine
	for i := 0; i < len(ls); {
		if ls[i].Op != ' ' {
			out = append(out, ls[i])
			i++
			continue
		}
		j := i
		for j < len(ls) && ls[j].Op == ' ' {
			j++
		}
		run := ls[i:j]
		head, tail := diffContext, diffContext
		if i == 0 {
			head = 0 // nothing changed before this run
		}
		if j == len(ls) {
			tail = 0 // nothing changed after it
		}
		if len(run) <= head+tail || i == 0 && j == len(ls) {
			out = append(out, run...)
		} else {
			out = append(out, run[:head]...)
			out = append(out, DiffLine{Op: '…'})
			out = append(out, run[len(run)-tail:]...)
		}
		i = j
	}
	return out
}

// bodyDiff is a description edit's stored change, for the ticket history:
// Show is false for any other event and for events from before the text
// was kept.
type bodyDiff struct {
	Show, TooLong bool
	Lines         []DiffLine
}

func bodyChanges(e EventView) bodyDiff {
	if e.Kind != "ticket.updated" {
		return bodyDiff{}
	}
	if long, _ := e.Payload["body_too_long"].(bool); long {
		return bodyDiff{Show: true, TooLong: true}
	}
	before, ok1 := e.Payload["body_before"].(string)
	after, ok2 := e.Payload["body_after"].(string)
	if !ok1 || !ok2 {
		return bodyDiff{}
	}
	return bodyDiff{Show: true, Lines: LineDiff(before, after)}
}

// diffLineClass colours a diff line by what happened to it.
func diffLineClass(op rune) string {
	switch op {
	case '-':
		return "block bg-danger/10 text-danger"
	case '+':
		return "block bg-accent/10 text-accent"
	}
	return "block text-muted"
}

// diffLineText is a diff line as shown: its mark, then the text.
func diffLineText(l DiffLine) string {
	switch l.Op {
	case '…':
		return "…"
	case ' ':
		return "  " + l.Text
	}
	return string(l.Op) + " " + l.Text
}
