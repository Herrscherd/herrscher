//go:build e2e

package runtimedir

import (
	"os"
	"path"
)

const e2eRootEnv = "HERRSCHER_E2E_RUNTIME_ROOT"

const unsetE2eRoot = "/herrscher-e2e-root-unset"

func productionRoots() searchRoots {
	root := os.Getenv(e2eRootEnv)
	if !path.IsAbs(root) {
		root = unsetE2eRoot
	}
	return searchRoots{runUser: root, tmp: root}
}
