//go:build windows

package worktree

import (
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

func resolveExistingPath(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	handle := windows.Handle(file.Fd())
	size, err := windows.GetFinalPathNameByHandle(handle, nil, 0, 0)
	if err != nil {
		return "", err
	}
	buffer := make([]uint16, size+1)
	size, err = windows.GetFinalPathNameByHandle(
		handle,
		&buffer[0],
		uint32(len(buffer)),
		0,
	)
	if err != nil {
		return "", err
	}
	resolved := windows.UTF16ToString(buffer[:size])
	if strings.HasPrefix(resolved, `\\?\UNC\`) {
		return `\\` + strings.TrimPrefix(resolved, `\\?\UNC\`), nil
	}
	return strings.TrimPrefix(resolved, `\\?\`), nil
}
