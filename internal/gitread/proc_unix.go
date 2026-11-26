//go:build unix

package gitread

import (
	"os/exec"
	"syscall"
)

// killGroup runs cmd in its own process group and kills the whole group
// when its context ends, so a helper git started (ssh, a remote helper)
// can't outlive the timeout.
func killGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
