package host

import (
	"os"
	"strings"

	"github.com/Herrscherd/herrscher/core/internal/control"
)

// commandSocketTarget is where a short-lived process dials the daemon's command
// socket. On the daemon's own machine that is CommandSocketPath, derived from
// the instance id the way it always was.
//
// On a machine carrying a remote session it is a forwarded path named after the
// session, which nothing over there can compute: the launch states it in the
// environment, the bridge reads it on stdin, and every process it spawns
// inherits it. So an agent's `herrscher <verb>` reaches the daemon that started
// it rather than a socket that does not exist on that machine.
func commandSocketTarget(instanceID string) string {
	return commandSocketTargetFrom(func() string { return instanceID })
}

// commandSocketTargetFrom is commandSocketTarget with the instance named only
// when it is needed. Naming it means reading the daemon's state file, which is
// not free and not read-only either, and the forwarded path in a session's
// environment answers without it. The one caller that pays that price per tool
// call takes this form; everyone else already holds the id.
func commandSocketTargetFrom(instanceID func() string) string {
	if p := strings.TrimSpace(os.Getenv(control.CommandSocketVar)); p != "" {
		return p
	}
	return CommandSocketPath(instanceID())
}
