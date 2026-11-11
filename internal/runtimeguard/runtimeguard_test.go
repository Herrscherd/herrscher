package runtimeguard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func oldTime() time.Time {
	return time.Date(2001, time.March, 4, 5, 6, 7, 0, time.UTC)
}

func TestGuardKeepsTheExitCodeWhenNothingChanged(t *testing.T) {
	guard := Start(t.TempDir())
	if got := guard.Finish(7); got != 7 {
		t.Fatalf("Finish(7) = %d on an untouched directory, want 7", got)
	}
}

type changeCase struct {
	name   string
	setup  func(dir string) error
	change func(dir string) error
	want   string
}

func TestGuardReportsAChangeToTheRuntimeDirectory(t *testing.T) {
	cases := []changeCase{
		{
			name:   "an entry is added",
			change: func(dir string) error { return writeFile(filepath.Join(dir, "b.sock"), "b", oldTime()) },
			want:   "b.sock: added",
		},
		{
			name:   "an entry is removed",
			setup:  func(dir string) error { return writeFile(filepath.Join(dir, "a.sock"), "a", oldTime()) },
			change: func(dir string) error { return os.Remove(filepath.Join(dir, "a.sock")) },
			want:   "a.sock: removed",
		},
		{
			name:   "an entry changes size",
			setup:  func(dir string) error { return writeFile(filepath.Join(dir, "a.sock"), "a", oldTime()) },
			change: func(dir string) error { return writeFile(filepath.Join(dir, "a.sock"), "aa", oldTime()) },
			want:   "a.sock: size changed",
		},
		{
			name:  "an entry changes mtime",
			setup: func(dir string) error { return writeFile(filepath.Join(dir, "a.sock"), "a", oldTime()) },
			change: func(dir string) error {
				return os.Chtimes(filepath.Join(dir, "a.sock"), oldTime().Add(time.Hour), oldTime().Add(time.Hour))
			},
			want: "a.sock: mtime changed",
		},
		{
			name:  "an entry is replaced under the same name, size and mtime",
			setup: func(dir string) error { return writeFile(filepath.Join(dir, "a.sock"), "a", oldTime()) },
			change: func(dir string) error {
				if err := writeFile(filepath.Join(dir, "a.tmp"), "a", oldTime()); err != nil {
					return err
				}
				return os.Rename(filepath.Join(dir, "a.tmp"), filepath.Join(dir, "a.sock"))
			},
			want: "a.sock: inode changed",
		},
		{
			name:   "the directory mtime moves with no entry change",
			change: func(dir string) error { return os.Chtimes(dir, oldTime().Add(time.Hour), oldTime().Add(time.Hour)) },
			want:   "the directory mtime moved",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertChangeIsReported(t, tc)
		})
	}
}

func assertChangeIsReported(t *testing.T, tc changeCase) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chtimes(dir, oldTime(), oldTime()); err != nil {
		t.Fatal(err)
	}
	if tc.setup != nil {
		if err := tc.setup(dir); err != nil {
			t.Fatal(err)
		}
	}
	guard := Start(dir)
	if err := tc.change(dir); err != nil {
		t.Fatal(err)
	}
	changes, err := guard.Changes()
	if err != nil {
		t.Fatal(err)
	}
	if !containsChange(changes, tc.want) {
		t.Fatalf("changes = %q, want one containing %q", changes, tc.want)
	}
	if got := guard.Finish(0); got != 1 {
		t.Fatalf("Finish(0) = %d after a change, want 1", got)
	}
}

func TestGuardReportsTheDirectoryBeingCreated(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "herrscher")
	guard := Start(dir)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	changes, err := guard.Changes()
	if err != nil {
		t.Fatal(err)
	}
	if !containsChange(changes, "the directory was created") {
		t.Fatalf("changes = %q, want the directory creation reported", changes)
	}
}

func writeFile(path, content string, mtime time.Time) error {
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return err
	}
	return os.Chtimes(path, mtime, mtime)
}

func containsChange(changes []string, want string) bool {
	for _, change := range changes {
		if strings.Contains(change, want) {
			return true
		}
	}
	return false
}

func TestATestLeakWithNoDaemonFailsTheGuard(t *testing.T) {
	dir := t.TempDir()
	guard := Start(dir)
	if err := writeFile(filepath.Join(dir, "leak.txt"), "leak", oldTime()); err != nil {
		t.Fatal(err)
	}
	if changes := changesOf(t, guard); !containsChange(changes, "leak.txt: added") {
		t.Fatalf("changes = %q, want the leaked file reported", changes)
	}
}

func changesOf(t *testing.T, guard *Guard) []string {
	t.Helper()
	changes, err := guard.Changes()
	if err != nil {
		t.Fatal(err)
	}
	return changes
}
