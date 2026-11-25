package archive

import "testing"

// SetExportedSettings replaces the exported-settings allow-list for one test
// and restores it on cleanup. Tests that call it must not run in parallel.
func SetExportedSettings(t testing.TB, keys ...string) {
	t.Helper()
	saved := exportedSettings
	exportedSettings = append([]string{}, keys...)
	t.Cleanup(func() { exportedSettings = saved })
}

// SetFreeBytes replaces the free-space reading Read checks for one test and
// restores it on cleanup.
func SetFreeBytes(t testing.TB, f func(string) (int64, error)) {
	t.Helper()
	saved := freeBytes
	freeBytes = f
	t.Cleanup(func() { freeBytes = saved })
}
