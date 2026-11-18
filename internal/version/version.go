// Package version carries build metadata stamped in via -ldflags -X.
package version

var (
	// Version is the release number without a leading "v", or "0.0.0-dev"
	// for a build made outside a tagged release.
	Version = "0.0.0-dev"
	// Commit is the git short sha, or "dev" for an unstamped build.
	Commit = "dev"
	// BuildDate is the UTC build moment in RFC3339, or "" when unstamped.
	BuildDate = ""
)

// Line is what "vaquero version" prints: the release, then the commit and,
// when the binary was stamped with one, the build date.
func Line() string {
	if BuildDate == "" {
		return "vaquero " + Version + " (" + Commit + ")"
	}
	return "vaquero " + Version + " (" + Commit + ", " + BuildDate + ")"
}
