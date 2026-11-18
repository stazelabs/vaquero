// Package idgen mints primary keys. ULIDs sort by creation time.
package idgen

import "github.com/oklog/ulid/v2"

// New returns a new 26-character ULID string.
func New() string { return ulid.Make().String() }
