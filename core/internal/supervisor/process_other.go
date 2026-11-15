//go:build !windows

package supervisor

import (
	"context"
	"os"
	"os/exec"
	"syscall"
)

func configureBridgeCommand(_ context.Context, cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		return cmd.Process.Signal(syscall.SIGTERM)
	}
	cmd.WaitDelay = BridgeTermGrace
}
