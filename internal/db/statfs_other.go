//go:build !linux

package db

import "syscall"

// blockSize is the unit Bavail counts in. On macOS statfs counts in Bsize.
func blockSize(st *syscall.Statfs_t) int64 { return int64(st.Bsize) } //nolint:gosec // sizes fit int64
