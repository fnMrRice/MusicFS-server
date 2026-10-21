//go:build !linux && !darwin && !windows

package pluginhost

import (
	"fmt"
	"os/exec"
)

func onlineConfigureProcess(cmd *exec.Cmd) {}
func onlineContainProcess(pid int) (func(), error) {
	return nil, fmt.Errorf("UNSUPPORTED: online process containment is unavailable")
}
