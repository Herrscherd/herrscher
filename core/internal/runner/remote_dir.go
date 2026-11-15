package runner

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/Herrscherd/herrscher/core/internal/runtimedir"
)

// ErrRemoteRuntimeDir reports that the private runtime directory of the remote
// user could not be created or is not safe to use.
var ErrRemoteRuntimeDir = errors.New("remote runtime directory cannot be prepared")

// RemoteRuntimeDir resolves the private runtime directory of the user on target
// over ssh, on every call, so a changed far side is seen at the next session
// start. Its socket paths on the far side are built from this directory, so they
// cannot be planted by another user there. controlPath is the multiplexing socket
// to reuse, or empty to run without multiplexing.
func RemoteRuntimeDir(ctx context.Context, target, controlPath string) (string, error) {
	if err := CheckTarget(target); err != nil {
		return "", fmt.Errorf("host %q: %w", target, err)
	}
	ssh := SSH{Target: target, ControlPath: controlPath}
	out, err := ssh.Command(ctx, "", "sh", "-c", runtimedir.RemoteDirScript()).Output()
	if err != nil {
		return "", fmt.Errorf("host %q: %w: %w", target, ErrRemoteRuntimeDir, commandError(err))
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	dir := lines[len(lines)-1]
	if !runtimedir.IsRemoteDir(dir) {
		return "", fmt.Errorf("host %q: %w: unexpected directory %q", target, ErrRemoteRuntimeDir, dir)
	}
	return dir, nil
}

func commandError(err error) error {
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		return fmt.Errorf("%w: %s", exitErr, strings.TrimSpace(string(exitErr.Stderr)))
	}
	return err
}
