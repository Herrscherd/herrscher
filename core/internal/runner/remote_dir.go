package runner

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path"
	"strings"

	"github.com/Herrscherd/herrscher/core/internal/runtimedir"
)

// ErrRemoteRuntimeDir reports that the private runtime directory of the remote
// user could not be created or is not safe to use.
var ErrRemoteRuntimeDir = errors.New("remote runtime directory cannot be prepared")

// RemoteRuntimeDirScript is the shell that prepares the private directory of the
// user on the far side under root, and prints its path. It refuses a directory
// that is a symlink, is not the user's, or is open to its group or others.
func RemoteRuntimeDirScript(root string) string {
	return fmt.Sprintf(`uid=$(id -u) && dir='%s'/herrscher-$uid && mkdir -m 0700 -p "$dir" && test -d "$dir" && test ! -L "$dir" && test -O "$dir" && test "$(ls -ld "$dir" | cut -c1-10)" = drwx------ && printf '%%s\n' "$dir"`, root)
}

// RemoteRuntimeDir resolves the private runtime directory of the user on target
// over ssh. Its socket paths on the far side are built from this directory, so
// they cannot be planted by another user there.
func RemoteRuntimeDir(ctx context.Context, target string) (string, error) {
	ssh := SSH{Target: target, ControlPath: ControlPathFor(target)}
	out, err := ssh.Command(ctx, "", "sh", "-c", RemoteRuntimeDirScript(runtimedir.Production().Root)).Output()
	if err != nil {
		return "", fmt.Errorf("host %q: %w: %w", target, ErrRemoteRuntimeDir, commandError(err))
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	dir := lines[len(lines)-1]
	if path.Dir(dir) != runtimedir.Production().Root {
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
