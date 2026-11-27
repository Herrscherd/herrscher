package worktree

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// SameFolderName reports whether two recorded folder paths name the same folder without looking at the disk: they
// match once cleaned, ignoring case on darwin and windows, whose default filesystems do.
func SameFolderName(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if caseInsensitiveFolders() {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// SameLocalFolder reports whether two folder paths of this machine name the same folder: by [SameFolderName], or,
// when both exist, by being the same file on disk, which also sees through links and case on any filesystem.
func SameLocalFolder(a, b string) bool {
	if SameFolderName(a, b) {
		return true
	}
	aInfo, err := os.Stat(a)
	if err != nil {
		return false
	}
	bInfo, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(aInfo, bInfo)
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
