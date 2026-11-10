//go:build !unix && !windows

package host

import "os"

func tryLock(*os.File) (bool, error) { return true, nil }

func lockInstance(string) (func(), error) { return func() {}, nil }
