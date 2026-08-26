//go:build !windows

package host

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"os"
	"path/filepath"

	"github.com/Herrscherd/herrscher/core/internal/control"
)

// CommandSocketPath is the daemon-level operator command socket.
func CommandSocketPath(instanceID string) string {
	name := "herrscher-command.sock"
	if instanceID != "" {
		name = "herrscher-command-" + instanceID + ".sock"
	}
	return filepath.Join(os.TempDir(), name)
}

// SessionCommandSocketPath is the command socket one session's own processes
// dial, distinct from the operator's.
//
// It is what tells the daemon who is calling. Locally the agent and the daemon
// share a uid, so no secret the agent can read distinguishes it from the
// operator: any token it could present, it could also have replayed. Where the
// connection arrives is the one thing an agent does not get to author, because
// the daemon chose which listener serves which caller.
//
// Locally that is a guardrail, not a wall: nothing stops a determined agent
// from looking for the operator socket in the same directory. Remotely it is a
// real boundary, since ssh forwards this session's socket and no other, so the
// far machine has no path to anything else.
func SessionCommandSocketPath(instanceID, session string) string {
	name := "herrscher-command"
	if instanceID != "" {
		name += "-" + instanceID
	}
	dir := os.TempDir()
	p := filepath.Join(dir, name+"-s-"+control.SafeSessionName(session)+".sock")
	if len(p) <= maxUnixSocketPath {
		return p
	}
	// sun_path caps a unix socket path, and this one has three variable parts:
	// TMPDIR, an instance id, and a session name that a free-text prompt can
	// slugify all the way to 64 characters. Over the cap, bind fails with
	// "invalid argument", and the cost of that is not a missing socket: the
	// session starts anyway, its `herrscher <verb>` finds nobody, and the CLI
	// falls back to running the verb in the agent's own process against the
	// state file. The boundary would disappear exactly where the name got long.
	//
	// So fold instead of failing. The digest is of the full session name, so two
	// names that share a prefix do not share a socket, and both the daemon and
	// the supervisor derive it here, so they cannot disagree about where it is.
	sum := sha256.Sum256([]byte(session))
	digest := hex.EncodeToString(sum[:])[:12]
	room := maxUnixSocketPath - len(filepath.Join(dir, name+"-s-"+"-"+digest+".sock"))
	if room < 0 {
		room = 0
	}
	short := control.SafeSessionName(session)
	if len(short) > room {
		short = short[:room]
	}
	return filepath.Join(dir, name+"-s-"+short+"-"+digest+".sock")
}

// maxUnixSocketPath is what sun_path holds, minus the terminating NUL. Linux
// gives 108 bytes and the BSDs 104, so the smaller one is the only figure that
// is true everywhere a herrscher daemon runs.
const maxUnixSocketPath = 103

// EventsSocketPath is the daemon-level per-session events fan-out socket: a
// sibling of the command socket (herrscher-command → herrscher-events). It is the path
// Neublox's HerrscherEventSource connects to, derived there the same way.
func EventsSocketPath(instanceID string) string {
	name := "herrscher-events.sock"
	if instanceID != "" {
		name = "herrscher-events-" + instanceID + ".sock"
	}
	return filepath.Join(os.TempDir(), name)
}

func listenCommandSocket(path string) (net.Listener, error) {
	l, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = l.Close()
		_ = os.Remove(path)
		return nil, err
	}
	return l, nil
}

func dialCommandSocket(ctx context.Context, path string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "unix", path)
}
