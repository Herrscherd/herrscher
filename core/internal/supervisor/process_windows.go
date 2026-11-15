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

func configureBridgeCommand(ctx context.Context, cmd *exec.Cmd) {
	directCancel := cmd.Cancel
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}

		treeErr := killProcessTree(ctx, cmd.Process.Pid)
		if treeErr == nil {
			return nil
		}
		if directCancel != nil {
			directErr := directCancel()
			if directErr == nil || errors.Is(directErr, os.ErrProcessDone) {
				return directErr
			}
			return errors.Join(treeErr, directErr)
		}
		return treeErr
	}
}

func killProcessTree(ctx context.Context, pid int) error {
	systemDir, err := windows.GetSystemDirectory()
	if err != nil {
		return err
	}
	killCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), processTreeKillTimeout)
	defer cancel()
	killer := exec.CommandContext(
		killCtx,
		filepath.Join(systemDir, "taskkill.exe"),
		"/PID", strconv.Itoa(pid), "/T", "/F",
	)
	killer.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return killer.Run()
}
