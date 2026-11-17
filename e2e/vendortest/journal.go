package vendortest

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func appendLine(path, line string) error {
	file, err := os.OpenFile(filepath.Clean(path), os.O_APPEND|os.O_CREATE|os.O_WRONLY, privateFile)
	if err != nil {
		return fmt.Errorf("cannot open the log: %w", err)
	}
	_, werr := fmt.Fprintln(file, line)
	return errors.Join(werr, file.Close())
}
