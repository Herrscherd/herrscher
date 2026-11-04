//go:build !unix

package tui

import "os"

const draftLocksSupported = false

func lockRunDir(string) (*os.File, error) {
	return nil, nil
}

func runDirHeld(string) bool {
	return false
}

func privateDir(info os.FileInfo) bool {
	return info.IsDir()
}
