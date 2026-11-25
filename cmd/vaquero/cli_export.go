package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/stazelabs/vaquero/internal/client"
	"github.com/stazelabs/vaquero/internal/units"
	"github.com/stazelabs/vaquero/internal/version"
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
	if min, need := c.client.UpgradeNeeded(); need {
		fmt.Fprintf(c.stderr, "This server needs vaquero %s or newer; this is %s. Upgrade vaquero.\n", min, version.Version)
	}
	if _, ok := errors.AsType[*client.Error](err); ok || err == nil {
		return n, err
	}
	return n, fmt.Errorf("%s (%w)%s", "The export broke off before it finished", err, ".")
}

// exportFile downloads into dest.tmp (0600: it holds everything), then
// renames it to dest.
func exportFile(ctx context.Context, c *cmd, dest string) (int64, error) {
	tmp := dest + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
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
		return 0, fmt.Errorf("%w %s", err, "Nothing was saved at "+dest+".")
	}
	return n, nil
}
