package logfile

import (
	"errors"
	"os"
)

// FailOpen makes every open of a log file fail until the returned func runs.
func FailOpen() (restore func()) {
	old := openFile
	openFile = func(string, int, os.FileMode) (*os.File, error) { return nil, errors.New("open refused") }
	return func() { openFile = old }
}
