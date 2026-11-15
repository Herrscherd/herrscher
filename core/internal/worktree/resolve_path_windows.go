//go:build windows

package worktree

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func resolveExistingPath(path string) (string, error) {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	resolved, resolveErr := finalPathName(file)
	return resolved, errors.Join(resolveErr, file.Close())
}

func finalPathName(file *os.File) (string, error) {
	handle := windows.Handle(file.Fd())
	size, err := windows.GetFinalPathNameByHandle(handle, nil, 0, 0)
	if err != nil {
		return "", err
	}
	bufferLen := size + 1
	buffer := make([]uint16, bufferLen)
	size, err = windows.GetFinalPathNameByHandle(handle, &buffer[0], bufferLen, 0)
	if err != nil {
		return "", err
	}
	resolved := windows.UTF16ToString(buffer[:size])
	if strings.HasPrefix(resolved, `\\?\UNC\`) {
		return `\\` + strings.TrimPrefix(resolved, `\\?\UNC\`), nil
	}
	return strings.TrimPrefix(resolved, `\\?\`), nil
}
