//go:build !windows

package host

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListenCommandSocketNeverPublishesARelaxedSocket(t *testing.T) {
	dir := t.TempDir()
	cases := []string{"a.sock", "b.sock"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name)
			watched := make(chan os.FileMode, 1)
			stop := make(chan struct{})
			go watchSocketMode(path, watched, stop)
			l, err := listenCommandSocket(t.Context(), path)
			close(stop)
			if err != nil {
				t.Fatalf("listenCommandSocket: %v", err)
			}
			defer func() { closeRead(l); logCleanup(os.Remove(path)) }()
			assertNeverRelaxed(t, watched)
			assertSocketMode(t, path, 0o600)
			assertNoStagingDirectory(t, dir)
		})
	}
}

func watchSocketMode(path string, watched chan<- os.FileMode, stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		default:
		}
		if fi, err := os.Lstat(path); err == nil && fi.Mode().Perm() != 0o600 {
			select {
			case watched <- fi.Mode().Perm():
			default:
			}
			return
		}
	}
}

func assertNeverRelaxed(t *testing.T, watched <-chan os.FileMode) {
	t.Helper()
	select {
	case perm := <-watched:
		t.Fatalf("socket was visible with mode %o", perm)
	default:
	}
}

func assertSocketMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}
	if fi.Mode().Perm() != want {
		t.Fatalf("final mode = %o, want %o", fi.Mode().Perm(), want)
	}
}

func assertNoStagingDirectory(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			t.Fatalf("staging directory left behind: %s", e.Name())
		}
	}
}
