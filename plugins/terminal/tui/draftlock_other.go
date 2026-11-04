//go:build !unix

package tui

import "os"

const draftLocksSupported = false

func acquireDraftLock(root *os.Root) (*os.File, error) {
	lock, err := root.OpenFile(draftLockName, os.O_CREATE|os.O_RDWR, draftLockMode)
	if err != nil {
		return nil, err
	}
	return nil, lock.Close()
}

func runDirHeld(*os.Root, string) bool {
	return false
}

func privateDir(info os.FileInfo) bool {
	return info.IsDir()
}
