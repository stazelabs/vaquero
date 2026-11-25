// Package idgen mints primary keys. ULIDs sort by creation time.
package idgen

import "github.com/oklog/ulid/v2"

// New returns a new 26-character ULID string.
func New() string { return ulid.Make().String() }

// Valid reports whether s is a ULID (26 Crockford base32 characters, in
// either case), as New mints.
func Valid(s string) bool {
	_, err := ulid.ParseStrict(s)
	return err == nil
}
