//go:build !unix

package host

import (
	"os"
	"path/filepath"
	"strconv"
)

func instanceLockPath(instanceID string) (string, error) {
	name := "herrscher-instance.lock"
	if instanceID != "" {
		name = "herrscher-instance-" + instanceID + ".lock"
	}
	return filepath.Join(instanceLockDir(), name), nil
}

func instanceLockDir() string {
	if cache, err := os.UserCacheDir(); err == nil {
		return filepath.Join(cache, "herrscher")
	}
	return filepath.Join(os.TempDir(), "herrscher-"+strconv.Itoa(os.Getuid()))
}

func openLockFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_RDWR, ownerOnlyFileMode)
}
