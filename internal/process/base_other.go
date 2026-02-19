//go:build !linux

package process

import "os/exec"

// configureSysProcAttr is a no-op on non-Linux platforms.
// Pdeathsig (parent-death signal) is a Linux-only kernel feature.
func configureSysProcAttr(_ *exec.Cmd) {}
