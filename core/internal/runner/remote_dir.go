package runner

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path"
	"strconv"
	"strings"

	"github.com/Herrscherd/herrscher/core/internal/runtimedir"
)

// ErrRemoteRuntimeDir reports that the private runtime directory of the remote
// user could not be created or is not safe to use.
var ErrRemoteRuntimeDir = errors.New("remote runtime directory cannot be prepared")

// RemoteRuntimeDirScript is the shell that prepares the private directory of the
// user on the far side and prints its path. The directory is
// <runUser>/<uid>/herrscher when the far side has a systemd runtime directory for
// the uid that is private to it, and <tmp>/herrscher-<uid> otherwise, the same
// rule runtimedir applies locally. It refuses a directory that is a symlink, is
// not the user's, or is open to its group or others.
func RemoteRuntimeDirScript(runUser, tmp string) string {
	return fmt.Sprintf(`uid=$(id -u) && run=%[1]s/$uid && if [ -d "$run" ] && [ ! -L "$run" ] && [ -O "$run" ] && [ "$(ls -ld "$run" | cut -c1-10)" = drwx------ ]; then dir=$run/herrscher; else dir=%[2]s/herrscher-$uid; fi && mkdir -m 0700 -p "$dir" && test -d "$dir" && test ! -L "$dir" && test -O "$dir" && test "$(ls -ld "$dir" | cut -c1-10)" = drwx------ && printf '%%s\n' "$dir"`, shellQuote(runUser), shellQuote(tmp))
}

// RemoteRuntimeDir resolves the private runtime directory of the user on target
// over ssh, on every call, so a changed far side is seen at the next session
// start. Its socket paths on the far side are built from this directory, so they
// cannot be planted by another user there. controlPath is the multiplexing socket
// to reuse, or empty to run without multiplexing.
func RemoteRuntimeDir(ctx context.Context, target, controlPath string) (string, error) {
	return remoteRuntimeDirIn(ctx, target, controlPath, runtimedir.ProductionRoots())
}

func remoteRuntimeDirIn(ctx context.Context, target, controlPath string, roots runtimedir.Roots) (string, error) {
	if err := CheckTarget(target); err != nil {
		return "", fmt.Errorf("host %q: %w", target, err)
	}
	ssh := SSH{Target: target, ControlPath: controlPath}
	out, err := ssh.Command(ctx, "", "sh", "-c", RemoteRuntimeDirScript(roots.RunUser, roots.Tmp)).Output()
	if err != nil {
		return "", fmt.Errorf("host %q: %w: %w", target, ErrRemoteRuntimeDir, commandError(err))
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	dir := lines[len(lines)-1]
	if !isRemoteRuntimeDir(dir, roots) {
		return "", fmt.Errorf("host %q: %w: unexpected directory %q", target, ErrRemoteRuntimeDir, dir)
	}
	return dir, nil
}

func isRemoteRuntimeDir(dir string, roots runtimedir.Roots) bool {
	if path.Dir(dir) == roots.Tmp {
		return strings.HasPrefix(path.Base(dir), "herrscher-")
	}
	parent := path.Dir(dir)
	_, uidErr := strconv.Atoi(path.Base(parent))
	return path.Base(dir) == "herrscher" && uidErr == nil && path.Dir(parent) == roots.RunUser
}

func commandError(err error) error {
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		return fmt.Errorf("%w: %s", exitErr, strings.TrimSpace(string(exitErr.Stderr)))
	}
	return err
}
