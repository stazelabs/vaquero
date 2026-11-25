package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"

	"github.com/stazelabs/vaquero/internal/api"
	"github.com/stazelabs/vaquero/internal/core"
	"github.com/stazelabs/vaquero/internal/units"
)

func init() {
	cliCommands["attach"] = cmdAttach
	cliCommands["attachments"] = cmdAttachments
	cliCommands["detach"] = cmdDetach
}

// errFileTooLarge is the server's own message for a file over the limit.
const errFileTooLarge = "Files can be at most 10 MB."

// attachSource is one file to upload: a name and where its bytes come from.
type attachSource struct {
	name string
	open func() (io.Reader, func(), error)
}

// cmdAttach is vaquero attach TICKET FILE...: upload each file and print
// its Markdown reference, to paste into a body or comment.
func cmdAttach(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	const usage = "vaquero attach TICKET FILE... [--name NAME]  (FILE - reads standard input; it needs --name)"
	c := newCmd(stdout, stderr, usage)
	name := c.fs.String("name", "", "the file's name, for standard input")
	pos, err := c.parse(args, 2, -1)
	var sources []attachSource
	if err == nil {
		sources, err = attachSources(pos[1:], *name, usage)
	}
	return c.connected(ctx, err, func() error {
		key, err := canon(pos[0])
		if err != nil {
			return err
		}
		var done []api.Attachment
		for _, s := range sources {
			r, closeFn, err := s.open()
			if err != nil {
				return err
			}
			var a api.Attachment
			err = c.client.Upload(ctx, "/api/v1/tickets/"+key+"/attachments", s.name, r, &a)
			closeFn()
			if err != nil {
				return fmt.Errorf("%s: %w", s.name, err)
			}
			done = append(done, a)
			if !c.jsonOut {
				fmt.Fprintln(c.stdout, a.Markdown())
			}
		}
		if c.jsonOut {
			c.emit(done, func() {})
		}
		return nil
	})
}

// attachSources checks every file before any is uploaded, so a refusal
// leaves nothing half done: it must exist, be a file and fit the limit.
func attachSources(files []string, name, usage string) ([]attachSource, error) {
	stdinUsed := false
	var out []attachSource
	for _, f := range files {
		if f != "-" {
			if name != "" {
				return nil, usageError{usage: usage, hint: "--name is for a file read from standard input"}
			}
			st, err := os.Stat(f)
			if err != nil {
				return nil, fmt.Errorf("can't read %s: %w", f, errUnwrapped(err))
			}
			if st.IsDir() {
				return nil, fmt.Errorf("%s is a directory", f)
			}
			if st.Size() > core.MaxAttachment {
				return nil, fmt.Errorf("%s: %s", filepath.Base(f), errFileTooLarge)
			}
			out = append(out, attachSource{name: filepath.Base(f), open: func() (io.Reader, func(), error) {
				fh, err := os.Open(f)
				if err != nil {
					return nil, nil, fmt.Errorf("can't read %s: %w", f, errUnwrapped(err))
				}
				return fh, func() { fh.Close() }, nil
			}})
			continue
		}
		if name == "" {
			return nil, usageError{usage: usage, hint: "a file read from standard input needs --name"}
		}
		if stdinUsed {
			return nil, usageError{usage: usage, hint: "standard input can be read once"}
		}
		stdinUsed = true
		b, err := readStdinFile()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		out = append(out, attachSource{name: name, open: func() (io.Reader, func(), error) {
			return bytes.NewReader(b), func() {}, nil
		}})
	}
	return out, nil
}

// readStdinFile reads standard input up to the attachment limit; more is
// refused with the server's message.
func readStdinFile() ([]byte, error) {
	r := stdin
	if r == nil {
		r = os.Stdin
	}
	b, err := io.ReadAll(io.LimitReader(r, core.MaxAttachment+1))
	if err != nil {
		return nil, err
	}
	if len(b) > core.MaxAttachment {
		return nil, fmt.Errorf("%s", errFileTooLarge)
	}
	return b, nil
}

// errUnwrapped is err without the "open path:" prefix of an *fs.PathError,
// which the message already names.
func errUnwrapped(err error) error {
	if pe, ok := errors.AsType[*os.PathError](err); ok {
		return pe.Err
	}
	return err
}

// cmdAttachments is vaquero attachments TICKET: the ticket's files.
func cmdAttachments(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	c := newCmd(stdout, stderr, "vaquero attachments TICKET")
	pos, err := c.parse(args, 1, 1)
	return c.connected(ctx, err, func() error {
		key, err := canon(pos[0])
		if err != nil {
			return err
		}
		var as []api.Attachment
		if err := c.call(ctx, "GET", "/api/v1/tickets/"+key+"/attachments", nil, &as); err != nil {
			return err
		}
		c.emit(as, func() {
			if len(as) == 0 {
				fmt.Fprintf(c.stdout, "%s has no attachments.\n", key)
				return
			}
			rows := [][]string{{"ID", "NAME", "SIZE", "BY", "WHEN"}}
			for _, a := range as {
				rows = append(rows, []string{a.ID, a.Name, units.SizeWords(a.Size), a.UploadedBy, untilWords(a.CreatedAt)})
			}
			c.table(rows)
		})
		return nil
	})
}

// cmdDetach is vaquero detach ID: remove an attachment.
func cmdDetach(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	c := newCmd(stdout, stderr, "vaquero detach ID")
	pos, err := c.parse(args, 1, 1)
	return c.connected(ctx, err, func() error {
		if err := c.call(ctx, "DELETE", "/api/v1/attachments/"+url.PathEscape(pos[0]), nil, nil); err != nil {
			return err
		}
		c.emit(map[string]string{"id": pos[0]}, func() { fmt.Fprintf(c.stdout, "Removed %s.\n", pos[0]) })
		return nil
	})
}
