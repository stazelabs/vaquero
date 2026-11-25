package logfile

import "syscall"

// dup points fd at the file open as from.
func dup(from, fd int) error { return syscall.Dup2(from, fd) }
