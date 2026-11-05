//go:build !unix && !windows

package host

import "os"

func tryLock(*os.File) (bool, error) { return true, nil }
