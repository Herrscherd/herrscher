package worktree

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// SameFolderName reports whether two recorded folder paths name the same folder without looking at the disk: they
// match once cleaned, ignoring case on darwin and windows, whose default filesystems do.
func SameFolderName(first, second string) bool {
	first, second = filepath.Clean(first), filepath.Clean(second)
	if caseInsensitiveFolders() {
		return strings.EqualFold(first, second)
	}
	return first == second
}

// SameLocalFolder reports whether two folder paths of this machine name the same folder: by [SameFolderName], or,
// when both exist, by being the same file on disk, which also sees through links and case on any filesystem.
func SameLocalFolder(first, second string) bool {
	if SameFolderName(first, second) {
		return true
	}
	firstInfo, err := os.Stat(first)
	if err != nil {
		return false
	}
	secondInfo, err := os.Stat(second)
	if err != nil {
		return false
	}
	return os.SameFile(firstInfo, secondInfo)
}

func caseInsensitiveFolders() bool {
	return runtime.GOOS == "darwin" || runtime.GOOS == "windows"
}

// Placement is a folder a session records, with the host it lives on, empty for this machine.
type Placement struct {
	Host string
	Path string
}

// Same reports whether two placements name one folder: same host, and the same folder by [SameLocalFolder] on this
// machine or by [SameFolderName] on another, whose disk is out of reach. An empty path names no folder.
func (p Placement) Same(other Placement) bool {
	if p.Path == "" || other.Path == "" || p.Host != other.Host {
		return false
	}
	if p.Host == "" {
		return SameLocalFolder(p.Path, other.Path)
	}
	return SameFolderName(p.Path, other.Path)
}
