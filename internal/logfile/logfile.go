// Package logfile keeps a background server's log from growing without
// limit: {data dir}/vaquero.log for a server the command line started, or
// the file brew services sends the service's output to. At Max bytes it
// moves to .1 (replacing the one before) and a new file starts. The command
// line rotates vaquero.log before starting a server (Rotate); a server
// writing to a log file rotates it as it goes (Writer). PathOf finds the
// file a descriptor writes to.
package logfile

import (
	"errors"
	"os"
	"sync"
)

// Max is the size at which the log rotates.
const Max = 10 << 20

// Rotate moves the file at path to path.1 when it holds max bytes or
// more. A missing file is no error.
func Rotate(path string, max int64) error {
	st, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if st.Size() < max {
		return nil
	}
	return os.Rename(path, path+".1")
}

// Writer appends to a log file, rotating it before a write would take it
// past max bytes. Safe for concurrent use.
type Writer struct {
	path string
	max  int64
	mu   sync.Mutex
	f    *os.File
	size int64
	fds  []int // descriptors pointed at each new file (Redirect)
}

// Redirect makes each rotation also point fds (a server's standard output
// and error, which the Go runtime writes a crash trace to) at the new file,
// so nothing is written to the old one after it moved.
func (w *Writer) Redirect(fds ...int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.fds = append(w.fds, fds...)
}

// Open opens (or creates, 0600) the log at path for appending.
func Open(path string, max int64) (*Writer, error) {
	w := &Writer{path: path, max: max}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}

// openFile is os.OpenFile; tests replace it.
var openFile = os.OpenFile

func (w *Writer) open() error {
	f, err := openFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	w.f, w.size = f, st.Size()
	return nil
}

// Write appends p, first rotating the file when p would take it past max.
// If the rotation fails the write goes to the file as it is; when the new
// file can't be opened, the old one moves back first, so the log keeps its
// name.
func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.size > 0 && w.size+int64(len(p)) > w.max {
		if err := os.Rename(w.path, w.path+".1"); err == nil {
			old := w.f
			if err := w.open(); err != nil {
				w.f = old // keep writing where it was, under its own name
				_ = os.Rename(w.path+".1", w.path)
			} else {
				old.Close()
				for _, fd := range w.fds {
					_ = dup(int(w.f.Fd()), fd) //nolint:gosec // descriptors fit int
				}
			}
		}
	}
	n, err := w.f.Write(p)
	w.size += int64(n)
	return n, err
}

// Close closes the file.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.f.Close()
}
