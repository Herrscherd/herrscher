package tui

import (
	"fmt"
	"os"
	"sync"
)

const keylogEnv = "HERRSCHER_KEYLOG"

var (
	keylogOnce sync.Once
	keylogFile *os.File
)

func keylog(format string, args ...any) {
	keylogOnce.Do(func() {
		path := os.Getenv(keylogEnv)
		if path == "" {
			return
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return
		}
		keylogFile = f
	})
	if keylogFile == nil {
		return
	}
	_, err := fmt.Fprintf(keylogFile, format+"\n", args...)
	bestEffort(err)
}
