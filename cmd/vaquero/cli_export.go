package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/stazelabs/vaquero/internal/client"
	"github.com/stazelabs/vaquero/internal/units"
)

func init() { cliCommands["export"] = cmdExport }

// cmdExport is vaquero export FILE|-: the whole instance as a .tar.gz. A
// file is written to FILE.tmp and renamed into place only when the whole
// archive arrived, so a refused or broken export leaves nothing at FILE.
func cmdExport(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	c := newCmd(stdout, stderr, "vaquero export FILE  (- writes to standard output)")
	pos, err := c.parse(args, 1, 1)
	return c.connected(ctx, err, func() error {
		dest := pos[0]
		if dest == "-" {
			_, err := c.download(ctx, c.stdout)
			return err
		}
		n, err := exportFile(ctx, c, dest)
		if err != nil {
			return err
		}
		c.emit(map[string]any{"file": dest, "bytes": n}, func() {
			fmt.Fprintf(c.stdout, "Exported to %s (%s).\n", dest, units.SizeWords(n))
		})
		return nil
	})
}

// download streams the export to w; after it, an outdated CLI says so.
func (c *cmd) download(ctx context.Context, w io.Writer) (int64, error) {
	n, err := c.client.Download(ctx, "/api/v1/export", w)
	c.tellUpgrade()
	if _, ok := errors.AsType[*client.Error](err); ok || err == nil {
		return n, err
	}
	return n, brokeOff{err}
}

// exportFile downloads into dest.tmp (0600: it holds everything), then
// renames it to dest.
func exportFile(ctx context.Context, c *cmd, dest string) (int64, error) {
	tmp := dest + ".tmp"
	// A .tmp left by an earlier run would keep its own mode when written over.
	if err := os.Remove(tmp); err != nil && !errors.Is(err, os.ErrNotExist) {
		return 0, fmt.Errorf("can't write %s: %w", tmp, errUnwrapped(err))
	}
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return 0, fmt.Errorf("can't write %s: %w", tmp, errUnwrapped(err))
	}
	n, err := c.download(ctx, f)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil && cerr != nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, dest)
	}
	if err != nil {
		os.Remove(tmp)
		return 0, notSaved{err: err, dest: dest}
	}
	return n, nil
}

// brokeOff is a download that ended before the whole archive arrived.
type brokeOff struct{ err error }

func (e brokeOff) Error() string {
	return "The export broke off before it finished (" + e.err.Error() + ")."
}
func (e brokeOff) Unwrap() error { return e.err }

// notSaved is a failed export to a file, which leaves nothing there.
type notSaved struct {
	err  error
	dest string
}

func (e notSaved) Error() string { return e.err.Error() + " Nothing was saved at " + e.dest + "." }
func (e notSaved) Unwrap() error { return e.err }
