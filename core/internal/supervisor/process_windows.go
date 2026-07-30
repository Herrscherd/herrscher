//go:build windows

package supervisor

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

const processTreeKillTimeout = 10 * time.Second

// configureBridgeCommand replaces CommandContext's direct-process cancellation
// with an absolute System32 taskkill invocation. /T is required on Windows:
// bridge children inherit the session worktree as cwd and can keep it locked
// after their direct parent is gone.
func configureBridgeCommand(cmd *exec.Cmd) {
	directCancel := cmd.Cancel
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}

		systemDir, pathErr := windows.GetSystemDirectory()
		if pathErr == nil {
			killCtx, cancel := context.WithTimeout(context.Background(), processTreeKillTimeout)
			killer := exec.CommandContext(
				killCtx,
				filepath.Join(systemDir, "taskkill.exe"),
				"/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F",
			)
			killer.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
			killErr := killer.Run()
			cancel()
			if killErr == nil {
				return nil
			}
			pathErr = killErr
		}

		// taskkill is a system binary and should always be available. Fall back
		// to CommandContext's direct kill so a damaged Windows installation
		// cannot wedge Stop forever, while preserving both errors for diagnosis.
		if directCancel != nil {
			directErr := directCancel()
			if directErr == nil || errors.Is(directErr, os.ErrProcessDone) {
				return directErr
			}
			return errors.Join(pathErr, directErr)
		}
		return pathErr
	}
}
