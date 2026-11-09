package outbox

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const testMaxBytes int64 = 1024

func writeOutboundFile(t *testing.T, dir, name string, size int) contracts.OutboundFile {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, make([]byte, size), 0o600); err != nil {
		t.Fatal(err)
	}
	return contracts.OutboundFile{Path: path, Name: name, ContentType: "image/png"}
}

type vetCase struct {
	name        string
	produce     func(t *testing.T, dir string) []contracts.OutboundFile
	wantFiles   int
	wantRefused []string
	wantCause   error
}

func TestVet(t *testing.T) {
	cases := []vetCase{
		{
			name: "two regular files are accepted",
			produce: func(t *testing.T, dir string) []contracts.OutboundFile {
				t.Helper()
				return []contracts.OutboundFile{writeOutboundFile(t, dir, "a.png", 8), writeOutboundFile(t, dir, "b.png", 8)}
			},
			wantFiles: 2,
		},
		{
			name: "file outside the outbox is refused",
			produce: func(t *testing.T, _ string) []contracts.OutboundFile {
				t.Helper()
				return []contracts.OutboundFile{writeOutboundFile(t, t.TempDir(), "leak.png", 8)}
			},
			wantRefused: []string{"leak.png"},
			wantCause:   contracts.ErrInvalidOutboundFile,
		},
		{
			name: "symbolic link leaving the outbox is refused",
			produce: func(t *testing.T, dir string) []contracts.OutboundFile {
				t.Helper()
				target := writeOutboundFile(t, t.TempDir(), "secret.png", 8)
				link := filepath.Join(dir, "link.png")
				if err := os.Symlink(target.Path, link); err != nil {
					t.Fatal(err)
				}
				return []contracts.OutboundFile{{Path: link, Name: "link.png", ContentType: "image/png"}}
			},
			wantRefused: []string{"link.png"},
			wantCause:   ErrOutsideOutbox,
		},
		{
			name: "symbolic link to a file inside the outbox is refused as not regular",
			produce: func(t *testing.T, dir string) []contracts.OutboundFile {
				t.Helper()
				target := writeOutboundFile(t, dir, "real.png", 8)
				link := filepath.Join(dir, "alias.png")
				if err := os.Symlink(target.Path, link); err != nil {
					t.Fatal(err)
				}
				return []contracts.OutboundFile{{Path: link, Name: "alias.png", ContentType: "image/png"}}
			},
			wantFiles:   0,
			wantRefused: []string{"alias.png"},
			wantCause:   ErrNotRegular,
		},
		{
			name: "directory is refused as not regular",
			produce: func(t *testing.T, dir string) []contracts.OutboundFile {
				t.Helper()
				sub := filepath.Join(dir, "sub.png")
				if err := os.Mkdir(sub, 0o700); err != nil {
					t.Fatal(err)
				}
				return []contracts.OutboundFile{{Path: sub, Name: "sub.png", ContentType: "image/png"}}
			},
			wantRefused: []string{"sub.png"},
			wantCause:   ErrNotRegular,
		},
		{
			name: "file larger than the maximum is refused",
			produce: func(t *testing.T, dir string) []contracts.OutboundFile {
				t.Helper()
				return []contracts.OutboundFile{writeOutboundFile(t, dir, "big.png", int(testMaxBytes)+1)}
			},
			wantRefused: []string{"big.png"},
			wantCause:   ErrTooLarge,
		},
		{
			name: "file exactly at the maximum is accepted",
			produce: func(t *testing.T, dir string) []contracts.OutboundFile {
				t.Helper()
				return []contracts.OutboundFile{writeOutboundFile(t, dir, "edge.png", int(testMaxBytes))}
			},
			wantFiles: 1,
		},
		{
			name: "files past the tenth are refused",
			produce: func(t *testing.T, dir string) []contracts.OutboundFile {
				t.Helper()
				files := make([]contracts.OutboundFile, 0, MaxFiles+1)
				for i := range MaxFiles + 1 {
					files = append(files, writeOutboundFile(t, dir, "f"+string(rune('a'+i))+".png", 8))
				}
				return files
			},
			wantFiles:   MaxFiles,
			wantRefused: []string{"fk.png"},
			wantCause:   ErrTooMany,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			accepted, refused := Vet(dir, tc.produce(t, dir), testMaxBytes)
			assertVet(t, tc, len(accepted), refused)
		})
	}
}

func assertVet(t *testing.T, tc vetCase, accepted int, refused []Refusal) {
	t.Helper()
	if accepted != tc.wantFiles {
		t.Fatalf("accepted %d files, want %d", accepted, tc.wantFiles)
	}
	names := make([]string, 0, len(refused))
	for _, refusal := range refused {
		names = append(names, refusal.Name)
		if tc.wantCause != nil && !errors.Is(refusal.Cause, tc.wantCause) {
			t.Fatalf("refusal of %q has cause %v, want %v", refusal.Name, refusal.Cause, tc.wantCause)
		}
	}
	if !slices.Equal(names, tc.wantRefused) {
		t.Fatalf("refused %v, want %v", names, tc.wantRefused)
	}
}

func TestNewTurnDir(t *testing.T) {
	cases := []struct {
		name    string
		session string
		wantErr error
	}{
		{name: "named session gets a turn directory", session: "chan-1"},
		{name: "empty session is refused", session: "", wantErr: ErrBadSession},
		{name: "dot is refused", session: ".", wantErr: ErrBadSession},
		{name: "parent reference is refused", session: "..", wantErr: ErrBadSession},
		{name: "separator is refused", session: "a/b", wantErr: ErrBadSession},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			dir, err := NewTurnDir(root, tc.session)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("NewTurnDir error = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			assertPrivateTurnDir(t, root, dir)
		})
	}
}

func assertPrivateTurnDir(t *testing.T, root, dir string) {
	t.Helper()
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Fatalf("turn directory mode = %v, want a 0700 directory", info.Mode())
	}
	if filepath.Dir(filepath.Dir(dir)) != root {
		t.Fatalf("turn directory %q is not under root/session", dir)
	}
}

func TestRemove(t *testing.T) {
	cases := []struct {
		name    string
		dir     func(root string) string
		wantErr error
		kept    bool
	}{
		{name: "turn directory under root is removed", dir: func(root string) string { return filepath.Join(root, "chan", "turn-1") }},
		{name: "directory outside root is kept", dir: func(string) string { return t.TempDir() }, wantErr: ErrOutsideOutbox, kept: true},
		{name: "root itself is kept", dir: func(root string) string { return root }, wantErr: ErrOutsideOutbox, kept: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			dir := tc.dir(root)
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			err := Remove(root, dir)
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("Remove error = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr == nil && err != nil {
				t.Fatal(err)
			}
			_, statErr := os.Stat(dir)
			if kept := statErr == nil; kept != tc.kept {
				t.Fatalf("directory kept = %v, want %v", kept, tc.kept)
			}
		})
	}
}

func TestPurgeRemovesOnlyOldTurns(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	old := filepath.Join(root, "chan", "turn-old")
	fresh := filepath.Join(root, "chan", "turn-fresh")
	for _, dir := range []string{old, fresh} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	past := now.Add(-MaxAge - time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}

	if err := Purge(root, now); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(old); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old turn directory survived the purge: %v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("fresh turn directory was purged: %v", err)
	}
}

func TestPurgeMissingRootIsNotAnError(t *testing.T) {
	if err := Purge(filepath.Join(t.TempDir(), "absent"), time.Now()); err != nil {
		t.Fatalf("Purge of a missing root = %v, want nil", err)
	}
}

func TestFrameRoundTrip(t *testing.T) {
	frame := Frame{
		Dir:     "/state/outbox/chan/turn-1",
		Files:   []contracts.OutboundFile{{Path: "/state/outbox/chan/turn-1/a.png", Name: "a.png", ContentType: "image/png"}},
		Refused: []string{"big.bin"},
	}
	value, err := Encode(frame)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(value)
	if err != nil {
		t.Fatal(err)
	}
	if got.Dir != frame.Dir || !slices.Equal(got.Refused, frame.Refused) || !slices.Equal(got.Files, frame.Files) {
		t.Fatalf("round trip = %+v, want %+v", got, frame)
	}
}
