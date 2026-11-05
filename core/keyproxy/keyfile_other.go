//go:build !unix

package keyproxy

import (
	"errors"
	"io/fs"
	"os"
)

var errKeyFileUnsupported = errors.New("key_file is not supported on this platform: use key_env or key_cmd")

func openKeyFile(string) (*os.File, error) {
	return nil, errKeyFileUnsupported
}

func checkKeyDirectory(string, fs.FileInfo, int64) error {
	return nil
}
