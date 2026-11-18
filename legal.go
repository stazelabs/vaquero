// Package vaquero holds what must ship with the binary from the repository
// root: the licence and the notice.
package vaquero

import _ "embed"

//go:embed LICENSE
var license string

//go:embed NOTICE
var notice string

// License is vaquero's licence text (Apache 2.0).
func License() string { return license }

// Notice is vaquero's NOTICE file.
func Notice() string { return notice }
