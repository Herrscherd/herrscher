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
	return strings.Map(func(char rune) rune {
		switch {
		case char >= 'a' && char <= 'z', char >= 'A' && char <= 'Z', char >= '0' && char <= '9', char == '-', char == '_':
			return char
		default:
			return '-'
		}
	}, session)
}

func instancePrefix(instanceID string) string {
	if instanceID == "" {
		return ""
	}
	return SafeSessionName(instanceID) + instanceSeparator
}

const instanceSeparator = "."

const remoteSocketDir = "/tmp"

// RemoteSocketPath is SocketPath for another machine. It carries the instance
// for the same reason SocketPath does: two daemons forwarding a session of one
// name to one host would otherwise fight over the same path.
func RemoteSocketPath(instanceID, session string) string {
	return BoundedSocketPath(remoteSocketDir, "herrscher-control-"+instancePrefix(instanceID)+SafeSessionName(session))
}

// RemoteCommandSocketPath is where a remote session's `herrscher <verb>` dials
// its own command socket, once the launch has forwarded it there.
//
// Its own, and not the daemon's operator socket: which socket a connection
// arrives on is what tells the daemon who is calling, so forwarding the
// operator's would hand every agent on every host the operator's authority.
//
// It is named after the session for a second reason, older than that one. Two
// sessions on one host would otherwise ask for one path: the second launch
// finds it taken and dies on ExitOnForwardFailure, and clearing it first cuts
// the first session's agent off from the daemon. One path per session costs a
// socket file and makes both failures unrepresentable.
func RemoteCommandSocketPath(instanceID, session string) string {
	return BoundedSocketPath(remoteSocketDir, "herrscher-command-"+instancePrefix(instanceID)+SafeSessionName(session))
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
