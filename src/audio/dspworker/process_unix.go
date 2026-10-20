//go:build linux || darwin

package dspworker

import (
	"os/exec"
	"syscall"
)

func configureProcess(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }
func containProcess(pid int) (func(), error) {
	return func() { syscall.Kill(-pid, syscall.SIGKILL) }, nil
}
