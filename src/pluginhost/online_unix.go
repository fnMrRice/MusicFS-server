//go:build linux || darwin

package pluginhost

import (
	"os/exec"
	"syscall"
)

func onlineConfigureProcess(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }
func onlineContainProcess(pid int) (func(), error) {
	return func() { _ = syscall.Kill(-pid, syscall.SIGKILL) }, nil
}
