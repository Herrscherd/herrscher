// Package control is the bridge's control channel: a per-session local socket
// (or named pipe on Windows) the daemon hub accepts on and the bridge dials.
package control

import "strings"

// SocketPath derives the per-session control socket path. Both the supervisor
// (which passes it to the bridge) and the daemon (which accepts on it) compute
// the same path from the session name. Characters unsafe in a filename are
// folded to "-".
func safeSessionName(session string) string {
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
	return remoteSocketDir + "/herrscher-control-" + safeSessionName(session) + ".sock"
}
