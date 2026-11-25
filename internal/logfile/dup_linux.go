package logfile

import "syscall"

// dup points fd at the file open as from (Dup3: linux/arm64 has no Dup2).
func dup(from, fd int) error { return syscall.Dup3(from, fd, 0) }
