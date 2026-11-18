package db

import "context"

// SetFreeBytes swaps the free-space reading for a test and returns the undo.
func SetFreeBytes(f func(string) (int64, error)) (restore func()) {
	old := freeBytes
	freeBytes = f
	return func() { freeBytes = old }
}

// SetVacuumInto swaps the copy step for a test and returns the undo.
func SetVacuumInto(f func(ctx context.Context, path, dest string) error) (restore func()) {
	old := vacuumInto
	vacuumInto = f
	return func() { vacuumInto = old }
}

// RealVacuumInto is the copy step Snapshot normally uses.
var RealVacuumInto = vacuumInto
