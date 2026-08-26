// Package control is the bridge's control channel: a per-session local socket
// (or named pipe on Windows) the daemon hub accepts on and the bridge dials.
package control

import "strings"

// SafeSessionName folds a session name down to what is safe in a filename:
// every character outside letters, digits, "-" and "_" becomes "-". Both the
// supervisor (which passes a socket path to the bridge) and the daemon (which
// accepts on it) derive their paths through here, so the same session name
// always names the same socket.
//
// Exported because core/host derives a per-session command socket the same way,
// and two foldings that could drift is two names for one socket.
func SafeSessionName(session string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return '-'
		}
	}, session)
}

// remoteSocketDir is where a socket lands on a machine that is not this one.
// It is fixed rather than taken from os.TempDir(), which reads THIS machine's
// environment and would name a directory the far side may not have. /tmp is
// also the shortest path every POSIX host has, and sun_path caps a socket path
// at 108 bytes.
const remoteSocketDir = "/tmp"

// RemoteSocketPath is SocketPath for another machine.
func RemoteSocketPath(session string) string {
	return remoteSocketDir + "/herrscher-control-" + SafeSessionName(session) + ".sock"
}

// RemoteCommandSocketPath is where a remote session's `herrscher <verb>` dials
// the daemon's operator command socket, once the launch has forwarded it there.
//
// It is named after the session and not after the daemon, even though every
// session forwards the very same socket back here. Two sessions on one host
// would otherwise ask for one path: the second launch finds it taken and dies
// on ExitOnForwardFailure, and clearing it first cuts the first session's agent
// off from the daemon. One path per session costs a socket file and makes both
// failures unrepresentable.
func RemoteCommandSocketPath(session string) string {
	return remoteSocketDir + "/herrscher-command-" + SafeSessionName(session) + ".sock"
}

// CommandSocketVar is how the far side learns that path: it cannot derive a
// per-session name from the instance id alone, so the launch states it in the
// environment the bridge reads on stdin and passes to everything it spawns.
const CommandSocketVar = "HERRSCHER_COMMAND_SOCKET"

// SessionVar tells a process spawned inside a session which session it belongs
// to. The approval hook needs it: a vendor CLI's own session id is the
// vendor's, and the daemon knows nothing about it. It lives here rather than in
// core/host because the supervisor is what sets it, and core/host imports the
// supervisor, not the other way round.
const SessionVar = "HERRSCHER_SESSION"
