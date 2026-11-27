//go:build windows

package mcpweight

import (
	"errors"
	"os"
	"os/exec"
)

func isolateProcessGroup(_ *exec.Cmd) {}

func killProcessTree(cmd *exec.Cmd) error {
	err := cmd.Process.Kill()
	if errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	return err
}
