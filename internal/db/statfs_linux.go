package db

import "syscall"

// blockSize is the unit Bavail counts in. On Linux that is Frsize, the
// fragment size; Bsize is only the preferred transfer size and can differ.
func blockSize(st *syscall.Statfs_t) int64 { return int64(st.Frsize) } //nolint:gosec // sizes fit int64
