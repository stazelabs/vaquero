package gitread

import "context"

// RunCommand runs git sub args in dir through the runner's checks, as any
// read does: the allow-list test's way in.
func RunCommand(r *Repos, ctx context.Context, dir, sub string, args ...string) ([]byte, error) {
	return r.call(ctx, dir, sub, args, MaxOutput)
}

// RunRaw runs a whole argument list (after "git") through the checks.
func RunRaw(r *Repos, ctx context.Context, dir string, argv []string) ([]byte, error) {
	return r.run(ctx, dir, argv, MaxOutput)
}

// Prefix is the guard arguments every command starts with.
func Prefix(r *Repos, dir string) []string { return r.prefix(dir, nil) }

// HelperReads are the two fixed config reads Fetch runs.
var HelperReads = helperReads
