//go:build !e2e

package runtimedir

func productionRoots() searchRoots {
	return searchRoots{runUser: "/run/user", tmp: "/tmp"}
}
