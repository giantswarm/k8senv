//go:build linux

package process

import (
	"os/exec"
	"syscall"
)

// configureSysProcAttr sets Linux-specific process attributes on cmd.
// Pdeathsig ensures the child process receives SIGTERM when its parent dies,
// preventing orphaned kine and kube-apiserver processes if the test binary
// is killed abruptly.
func configureSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Pdeathsig: syscall.SIGTERM,
	}
}
