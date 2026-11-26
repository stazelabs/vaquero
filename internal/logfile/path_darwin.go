package logfile

import (
	"bytes"
	"os"
	"syscall"
	"unsafe"
)

// PathOf is the path of the file f has open (fcntl F_GETPATH).
func PathOf(f *os.File) (string, error) {
	buf := make([]byte, 1024) // MAXPATHLEN
	_, _, e := syscall.Syscall(syscall.SYS_FCNTL, f.Fd(), syscall.F_GETPATH, uintptr(unsafe.Pointer(&buf[0])))
	if e != 0 {
		return "", e
	}
	if i := bytes.IndexByte(buf, 0); i >= 0 {
		buf = buf[:i]
	}
	return string(buf), nil
}
