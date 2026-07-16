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
