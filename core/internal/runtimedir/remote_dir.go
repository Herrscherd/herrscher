package runtimedir

import (
	"fmt"
	"path"
	"strconv"
	"strings"
)

// RemoteDirScript is the shell that prepares the private directory of the user on
// a far side and prints its path. The directory is <run user>/<uid>/herrscher when
// that far side has a systemd runtime directory for the uid that is private to
// it, and /tmp/herrscher-<uid> otherwise, the same rule Production applies here.
// It refuses a directory that is a symlink, is not the user's, or is open to its
// group or others.
func RemoteDirScript() string {
	return remoteDirScript(productionRoots())
}

// IsRemoteDir reports whether dir is one of the two shapes RemoteDirScript prints.
func IsRemoteDir(dir string) bool {
	return isRemoteDir(dir, productionRoots())
}

func remoteDirScript(roots searchRoots) string {
	return fmt.Sprintf(`uid=$(id -u) && run=%[1]s/$uid && if [ -d "$run" ] && [ ! -L "$run" ] && [ -O "$run" ] && [ "$(ls -ld "$run" | cut -c1-10)" = drwx------ ]; then dir=$run/herrscher; else dir=%[2]s/herrscher-$uid; fi && mkdir -m 0700 -p "$dir" && test -d "$dir" && test ! -L "$dir" && test -O "$dir" && test "$(ls -ld "$dir" | cut -c1-10)" = drwx------ && printf '%%s\n' "$dir"`, shellQuote(roots.runUser), shellQuote(roots.tmp))
}

func isRemoteDir(dir string, roots searchRoots) bool {
	if path.Dir(dir) == roots.tmp {
		return strings.HasPrefix(path.Base(dir), dirPrefix)
	}
	parent := path.Dir(dir)
	_, uidErr := strconv.Atoi(path.Base(parent))
	return path.Base(dir) == runUserDirName && uidErr == nil && path.Dir(parent) == roots.runUser
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
