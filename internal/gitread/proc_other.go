//go:build !unix

package gitread

import "os/exec"

// killGroup leaves cmd's default cancel (kill the process) where process
// groups don't exist. Vaquero supports macOS and Linux.
func killGroup(*exec.Cmd) {}
