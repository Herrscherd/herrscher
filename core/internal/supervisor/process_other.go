//go:build !windows

package supervisor

import "os/exec"

func configureBridgeCommand(_ *exec.Cmd) {}
