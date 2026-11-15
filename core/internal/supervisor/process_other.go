//go:build !windows

// Package supervisor runs and supervises the bridge processes of herrscher sessions.
package supervisor

import (
	"os"
	"os/exec"
	"syscall"
	"time"
)

const bridgeTermGrace = 3 * time.Second

func configureBridgeCommand(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		return cmd.Process.Signal(syscall.SIGTERM)
	}
	cmd.WaitDelay = bridgeTermGrace
}
