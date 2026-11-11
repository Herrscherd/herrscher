//go:build !unix

package runtimeguard

const (
	daemonSockets = false
	holdersKnown  = false
)

func socketHolders(string) (map[string]bool, error) {
	return nil, nil
}
