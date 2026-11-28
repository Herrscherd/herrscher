//go:build windows

package devices

import (
	"errors"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func syncDirectory(dir string) error {
	name, err := windows.UTF16PtrFromString(filepath.Clean(dir))
	if err != nil {
		return err
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return err
	}
	return errors.Join(windows.FlushFileBuffers(handle), windows.CloseHandle(handle))
}
