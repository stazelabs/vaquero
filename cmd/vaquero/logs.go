package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// `vaquero logs` prints the local server's log, with no server needed:
// {data dir}/vaquero.log for a server the command line started, or the
// brew service's log, {brew prefix}/var/log/vaquero.log (the formula's
// log_path), when brew services runs Vaquero on this data directory.
func init() {
	cliCommands["logs"] = cmdLogs
}

const logsUsage = "vaquero logs [-n 100] [-f] [--path]"

// Hooks for tests: the running binary's path (where Homebrew's prefix is
// read from), how often -f looks for more, and how much of the file the
// tail reads at a time.
var (
	executable       = os.Executable
	followPoll       = 250 * time.Millisecond
	tailChunk  int64 = 64 << 10
)

// cmdLogs is `vaquero logs [-n N] [-f] [--path]`.
func cmdLogs(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	c := newCmd(stdout, stderr, logsUsage)
	n := c.fs.Int("n", 100, "how many of the last lines to print")
	follow := c.fs.Bool("f", false, "keep printing lines as they are written")
	pathOnly := c.fs.Bool("path", false, "print only the log's path")
	if _, err := c.parse(args, 0, 0); err != nil {
		return c.finish(err)
	}
	if *n < 0 {
		return c.finish(usageError{usage: logsUsage})
	}
	cfg, err := localConfig()
	if err != nil {
		return c.finish(err)
	}
	path, err := logPath(cfg.DataDir)
	if err != nil {
		return c.finish(err)
	}
	if *pathOnly {
		fmt.Fprintln(stdout, path)
		return exitOK
	}
	if !*follow {
		return c.finish(printTail(stdout, path, *n))
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	return c.finish(followLog(ctx, stdout, path, *n))
}

// logPath is where the server on dataDir writes its log.
func logPath(dataDir string) (string, error) {
	if _, ok := brewService(dataDir); !ok {
		return filepath.Join(dataDir, "vaquero.log"), nil
	}
	prefix, err := brewPrefix()
	if err != nil {
		return "", err
	}
	return filepath.Join(prefix, "var", "log", "vaquero.log"), nil
}

// brewPrefix is Homebrew's prefix: read from this binary's path when
// Homebrew installed it ({prefix}/Cellar/vaquero/… or {prefix}/opt/vaquero/…,
// symlinks followed, and only a prefix with a Cellar or var directory),
// else `brew --prefix`, bounded like every probe.
func brewPrefix() (string, error) {
	if exe, err := executable(); err == nil {
		paths := []string{exe}
		if real, err := filepath.EvalSymlinks(exe); err == nil && real != exe {
			paths = append(paths, real)
		}
		for _, p := range paths {
			for _, mark := range []string{"/Cellar/vaquero/", "/opt/vaquero/"} {
				if i := strings.Index(p, mark); i > 0 && homebrewPrefix(p[:i]) {
					return p[:i], nil
				}
			}
		}
	}
	out, err := serviceProbe("brew", "--prefix")
	if p := strings.TrimSpace(string(out)); err == nil && filepath.IsAbs(p) {
		return p, nil
	}
	return "", message("Vaquero runs under brew services, but Homebrew's prefix couldn't be found with brew --prefix; the log is var/log/vaquero.log under it.")
}

// printTail prints the last n lines of the log at path, ending with a
// newline.
func printTail(w io.Writer, path string, n int) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return message("No log at " + path + " yet.")
	}
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	b, err := tailLines(f, st.Size(), n)
	if err != nil {
		return err
	}
	if len(b) > 0 && b[len(b)-1] != '\n' {
		b = append(b, '\n')
	}
	_, err = w.Write(b)
	return err
}

// tailLines is the last n lines of the first size bytes of f, read from
// the end tailChunk bytes at a time. A newline at the very end ends the
// last line; it doesn't start another.
func tailLines(f io.ReaderAt, size int64, n int) ([]byte, error) {
	if n <= 0 || size <= 0 {
		return nil, nil
	}
	start, found := int64(0), 0
	buf := make([]byte, tailChunk)
	for pos := size; pos > 0 && found < n; {
		k := min(tailChunk, pos)
		pos -= k
		if _, err := f.ReadAt(buf[:k], pos); err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		for i := k - 1; i >= 0; i-- {
			if buf[i] != '\n' || pos+i == size-1 {
				continue
			}
			if found++; found == n {
				start = pos + i + 1
				break
			}
		}
	}
	out := make([]byte, size-start)
	if _, err := f.ReadAt(out, start); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return out, nil
}

// followLog prints the last n lines of the log at path, then every line
// written after them until ctx ends, waiting for the file when it isn't
// there yet. A rotation (the file moved to .1 and a new one started) is
// followed: the rest of the old file first, then the new one from its
// start.
func followLog(ctx context.Context, w io.Writer, path string, n int) error {
	var f *os.File
	defer func() {
		if f != nil {
			f.Close()
		}
	}()
	if g, err := os.Open(path); err == nil {
		f = g
		st, err := f.Stat()
		if err != nil {
			return err
		}
		b, err := tailLines(f, st.Size(), n)
		if err != nil {
			return err
		}
		if _, err := w.Write(b); err != nil {
			return err
		}
		if _, err := f.Seek(st.Size(), io.SeekStart); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	wait := func() bool {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(followPoll):
			return true
		}
	}
	for {
		if f != nil {
			if _, err := io.Copy(w, f); err != nil {
				return err
			}
		}
		st, err := os.Stat(path)
		switch {
		case errors.Is(err, os.ErrNotExist):
			// Moved and not started again yet, or not there at all.
		case err != nil:
			return err
		case f == nil:
			if f, err = openLog(path); err != nil {
				return err
			}
			continue
		default:
			cur, err := f.Stat()
			if err != nil {
				return err
			}
			if !os.SameFile(cur, st) {
				// Rotated: a write that was under way when the file moved
				// may still land in the old one, so read it once more after
				// a moment, then go on with the new file.
				if !wait() {
					return nil
				}
				if _, err := io.Copy(w, f); err != nil {
					return err
				}
				f.Close()
				if f, err = openLog(path); err != nil {
					return err
				}
				continue
			}
			if off, err := f.Seek(0, io.SeekCurrent); err == nil && off > st.Size() {
				// Cut short in place: read it again from the start.
				if _, err := f.Seek(0, io.SeekStart); err != nil {
					return err
				}
				continue
			}
		}
		if !wait() {
			return nil
		}
	}
}

// openLog opens the log at path; a missing one is nil, no error.
func openLog(path string) (*os.File, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return f, err
}

// homebrewPrefix reports whether dir looks like Homebrew's prefix: it has
// a Cellar or a var directory.
func homebrewPrefix(dir string) bool {
	for _, sub := range []string{"Cellar", "var"} {
		if st, err := os.Stat(filepath.Join(dir, sub)); err == nil && st.IsDir() {
			return true
		}
	}
	return false
}
