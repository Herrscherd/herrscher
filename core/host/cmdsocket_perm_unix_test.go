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
			go func() {
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
			}()
			l, err := listenCommandSocket(path)
			close(stop)
			if err != nil {
				t.Fatalf("listenCommandSocket: %v", err)
			}
			defer func() { _ = l.Close(); _ = os.Remove(path) }()
			select {
			case perm := <-watched:
				t.Fatalf("socket was visible with mode %o", perm)
			default:
			}
			fi, err := os.Lstat(path)
			if err != nil {
				t.Fatalf("lstat: %v", err)
			}
			if fi.Mode().Perm() != 0o600 {
				t.Fatalf("final mode = %o, want 600", fi.Mode().Perm())
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				if e.IsDir() {
					t.Fatalf("staging directory left behind: %s", e.Name())
				}
			}
		})
	}
}
