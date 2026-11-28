//go:build unix

package discovery

import (
	"context"
	"errors"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestAnSSHConfigNamedPipeIsRefusedWithoutBlocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	outcome := make(chan error, 1)
	go func() {
		_, err := SSHConfig{Path: path}.Find(context.Background())
		outcome <- err
	}()
	select {
	case err := <-outcome:
		if !errors.Is(err, ErrSSHConfigNotRegular) {
			t.Fatalf("got %v, want ErrSSHConfigNotRegular", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reading a named pipe as ssh configuration blocked")
	}
}
