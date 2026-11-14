//go:build e2e && linux

package e2e_test

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestReapInstanceKillsAndNamesWhatCarriesTheInstance(t *testing.T) {
	instance, err := newInstanceID()
	if err != nil {
		t.Fatal(err)
	}
	carrier := exec.CommandContext(t.Context(), "sleep", "60")
	carrier.Env = []string{instanceEnv + "=" + instance}
	bystander := exec.CommandContext(t.Context(), "sleep", "60")
	bystander.Env = []string{instanceEnv + "=" + instance + "-other"}
	for _, cmd := range []*exec.Cmd{carrier, bystander} {
		if startErr := cmd.Start(); startErr != nil {
			t.Fatal(startErr)
		}
	}
	leaked, err := reapInstance(instance, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(leaked) != 1 || !strings.Contains(leaked[0], "sleep 60") {
		t.Fatalf("got %q, want the one carrier named", leaked)
	}
	waitErr := carrier.Wait()
	if exit, ok := errors.AsType[*exec.ExitError](waitErr); !ok || exit.ExitCode() != -1 {
		t.Fatalf("carrier: got %v, want killed", waitErr)
	}
	if err := bystander.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("the bystander was touched: %v", err)
	}
}

func TestReapInstanceWaitsOutTheGrace(t *testing.T) {
	instance, err := newInstanceID()
	if err != nil {
		t.Fatal(err)
	}
	brief := exec.CommandContext(t.Context(), "sleep", "0.2")
	brief.Env = []string{instanceEnv + "=" + instance}
	if startErr := brief.Start(); startErr != nil {
		t.Fatal(startErr)
	}
	exited := make(chan error, 1)
	go func() { exited <- brief.Wait() }()
	leaked, err := reapInstance(instance, reapGrace)
	if err != nil || len(leaked) != 0 {
		t.Fatalf("got %q, %v, want nothing leaked", leaked, err)
	}
	if waitErr := <-exited; waitErr != nil {
		t.Fatalf("the process was killed instead of waited for: %v", waitErr)
	}
}

func TestProcessesInReportsAnUnreadableEnvironment(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "7", "environ"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := processesIn(root, []byte("x")); !errors.Is(err, syscall.EISDIR) {
		t.Fatalf("got %v, want the read failure", err)
	}
	if _, err := processesIn(filepath.Join(root, "missing"), []byte("x")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("got %v, want the listing failure", err)
	}
}

func TestSettleFailsOnALeakAndKillsIt(t *testing.T) {
	instance, err := newInstanceID()
	if err != nil {
		t.Fatal(err)
	}
	leaker := exec.CommandContext(t.Context(), "sleep", "60")
	leaker.Env = []string{instanceEnv + "=" + instance}
	if startErr := leaker.Start(); startErr != nil {
		t.Fatal(startErr)
	}
	before, err := unreadableEntries()
	if err != nil {
		t.Fatal(err)
	}
	if err := settle(instance, before); !errors.Is(err, errLeaked) {
		t.Fatalf("got %v, want the leak reported", err)
	}
	if waitErr := leaker.Wait(); waitErr == nil {
		t.Fatal("the leaked process was not killed")
	}
}

func privateDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func refusedRuntimeDirs(t *testing.T) (string, []string) {
	t.Helper()
	root := privateDir(t)
	file := filepath.Join(root, "file")
	touch(t, file)
	shared := filepath.Join(root, "shared")
	if err := os.Mkdir(shared, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(shared, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(privateDir(t), link); err != nil {
		t.Fatal(err)
	}
	return file, []string{shared, link}
}

func TestRemoveInstanceEntriesLeavesWhatIsNotAPrivateDirectoryAlone(t *testing.T) {
	instance, err := newInstanceID()
	if err != nil {
		t.Fatal(err)
	}
	file, dirs := refusedRuntimeDirs(t)
	if failures := removeInstanceEntries(file, instance); len(failures) == 0 {
		t.Errorf("the file %s was accepted as a runtime directory", file)
	}
	for _, dir := range dirs {
		entry := filepath.Join(dir, "herrscher-command-"+instance+".sock")
		touch(t, entry)
		if failures := removeInstanceEntries(dir, instance); len(failures) == 0 {
			t.Errorf("%s was accepted as a private runtime directory", dir)
		}
		if !exists(entry) {
			t.Errorf("%s was removed", entry)
		}
	}
}

func shortPrivateDir(t *testing.T, instance string) string {
	t.Helper()
	dir := filepath.Join(tmpRoot, "e2e-"+instance)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	return dir
}

func hashedRuntimeDir(t *testing.T, instance string) string {
	t.Helper()
	base := shortPrivateDir(t, instance)
	for pad := 1; ; pad++ {
		dir := filepath.Join(base, strings.Repeat("d", pad))
		known, err := entriesOf(dir, instance)
		if err != nil {
			t.Fatalf("no directory under %s gives hashed socket names before it is too long: %v", base, err)
		}
		if strings.HasPrefix(filepath.Base(known.command), "herrscher-") || strings.HasPrefix(filepath.Base(known.events), "herrscher-") {
			continue
		}
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		return dir
	}
}

func entriesIn(t *testing.T, dir, instance string) []string {
	t.Helper()
	known, err := entriesOf(dir, instance)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range known.paths() {
		touch(t, path)
	}
	return known.paths()
}

func TestRemoveHashedEntries(t *testing.T) {
	instance, err := newInstanceID()
	if err != nil {
		t.Fatal(err)
	}
	dir := hashedRuntimeDir(t, instance)
	ours := entriesIn(t, dir, instance)
	theirs := entriesIn(t, dir, "e2e-00000000")
	if failures := removeInstanceEntries(dir, instance); len(failures) != 0 {
		t.Fatal(failures)
	}
	for _, path := range ours {
		if exists(path) {
			t.Errorf("%s was left behind", path)
		}
	}
	for _, path := range theirs {
		if !exists(path) {
			t.Errorf("%s of another instance was removed", path)
		}
	}
}

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSettleNamesAHashedEntryThatAppeared(t *testing.T) {
	dir := privateDir(t)
	touch(t, filepath.Join(dir, "hs-000000000000000000000000.sock"))
	dirs := []string{dir, filepath.Join(dir, "missing")}
	before, err := unreadableEntriesIn(dirs)
	if err != nil {
		t.Fatal(err)
	}
	touch(t, filepath.Join(dir, "herrscher-command-x.sock"))
	touch(t, filepath.Join(dir, "3f9a0c1d2e4b5a6978"))
	if quiet := appearedUnreadableIn(dirs, before); quiet != nil {
		t.Fatalf("a readable name, a staged name or an older entry was reported: %v", quiet)
	}
	fresh := filepath.Join(dir, "hs-0123456789abcdef01234567.sock")
	touch(t, fresh)
	reported := appearedUnreadableIn(dirs, before)
	if !errors.Is(reported, errUnreadableEntry) || !strings.Contains(reported.Error(), fresh) {
		t.Fatalf("got %v, want %s reported", reported, fresh)
	}
}
