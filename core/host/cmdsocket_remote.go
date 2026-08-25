package host

// RemoteCommandSocketPath is CommandSocketPath for another machine. Same
// reasoning as control.RemoteSocketPath: the directory is fixed at /tmp rather
// than read from this machine's environment, which names a directory the far
// side may not have. It carries no build tag, unlike its local counterpart:
// where the socket lands over there does not depend on the daemon's own OS.
func RemoteCommandSocketPath(instanceID string) string {
	if instanceID == "" {
		return "/tmp/herrscher-command.sock"
	}
	return "/tmp/herrscher-command-" + instanceID + ".sock"
}
