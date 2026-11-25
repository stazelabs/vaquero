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
