package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const keylogEnv = "HERRSCHER_KEYLOG"

type keyLog struct {
	once sync.Once
	file *os.File
}

func (k *keyLog) write(format string, args ...any) {
	if k == nil {
		return
	}
	k.once.Do(func() {
		path := os.Getenv(keylogEnv)
		if path == "" {
			return
		}
		file, err := os.OpenFile(filepath.Clean(path), os.O_CREATE|os.O_WRONLY|os.O_APPEND, privateFileMode)
		if err != nil {
			return
		}
		k.file = file
	})
	if k.file == nil {
		return
	}
	_, err := fmt.Fprintf(k.file, format+"\n", args...)
	bestEffort(err)
}
