//go:build !windows

// Package supervisor runs and supervises the bridge processes of herrscher sessions.
package supervisor

import "os/exec"

func configureBridgeCommand(_ *exec.Cmd) {}
