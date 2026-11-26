package host

import (
	"os"
	"strings"

	"github.com/Herrscherd/herrscher/core/internal/control"
)

func commandSocketTarget(layout RuntimeLayout, instanceID string) string {
	return commandSocketTargetFrom(layout, func() string { return instanceID })
}

func commandSocketTargetFrom(layout RuntimeLayout, instanceID func() string) string {
	if p := strings.TrimSpace(os.Getenv(control.CommandSocketVar)); p != "" {
		return p
	}
	return servedSocket(layout, instanceID(), CommandSocketPathIn)
}

func servedSocket(layout RuntimeLayout, instanceID string, pathOf func(RuntimeLayout, string) string) string {
	if instanceID == "" {
		return ""
	}
	return pathOf(layout, instanceID)
}
