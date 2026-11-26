package logfile

import (
	"os"
	"strconv"
)

// PathOf is the path of the file f has open (/proc/self/fd).
func PathOf(f *os.File) (string, error) {
	return os.Readlink("/proc/self/fd/" + strconv.FormatUint(uint64(f.Fd()), 10))
}
