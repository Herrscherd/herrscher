//go:build unix && !linux

package runtimeguard

const (
	daemonSockets = true
	holdersKnown  = false
)

func socketHolders(string) (map[string]bool, error) {
	return nil, nil
}
