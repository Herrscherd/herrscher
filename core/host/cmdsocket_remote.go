package host

import (
	"os"
	"strings"

	"github.com/Herrscherd/herrscher/core/internal/control"
)

func commandSocketTarget(instanceID string) string {
	return commandSocketTargetFrom(func() string { return instanceID })
}

func commandSocketTargetFrom(instanceID func() string) string {
	if p := strings.TrimSpace(os.Getenv(control.CommandSocketVar)); p != "" {
		return p
	}
	return CommandSocketPath(instanceID())
}
