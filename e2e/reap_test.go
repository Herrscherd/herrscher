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
	if err := settle(instance); !errors.Is(err, errLeaked) {
		t.Fatalf("got %v, want the leak reported", err)
	}
	if waitErr := leaker.Wait(); waitErr == nil {
		t.Fatal("the leaked process was not killed")
	}
}

func TestRequireOwnedByUsRefusesWhatIsNotOurDirectory(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "file")
	link := filepath.Join(root, "link")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := requireOwnedByUs(root); err != nil {
		t.Fatalf("our own directory was refused: %v", err)
	}
	if _, isForeign := errors.AsType[*foreignDirError](requireOwnedBy(root, os.Getuid()+1)); !isForeign {
		t.Fatal("a directory owned by another uid was accepted")
	}
	shared := filepath.Join(root, "shared")
	if err := os.Mkdir(shared, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(shared, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, isForeign := errors.AsType[*foreignDirError](requireOwnedByUs(shared)); !isForeign {
		t.Fatal("a directory other users can enter was accepted")
	}
	for _, foreign := range []string{file, link} {
		refused, isForeign := errors.AsType[*foreignDirError](requireOwnedByUs(foreign))
		if !isForeign || refused.Dir != foreign {
			t.Fatalf("%s: got %v, want it refused", foreign, refused)
		}
	}
}

func TestRemoveInstanceEntriesLeavesAForeignDirectoryAlone(t *testing.T) {
	instance, err := newInstanceID()
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	entry := filepath.Join(target, "herrscher-command-"+instance+".sock")
	if err := os.WriteFile(entry, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	failures := removeInstanceEntries(link, instance)
	if _, isForeign := errors.AsType[*foreignDirError](errors.Join(failures...)); !isForeign {
		t.Fatalf("got %v, want the link refused", failures)
	}
	if _, err := os.Stat(entry); err != nil {
		t.Fatalf("an entry behind the link was removed: %v", err)
	}
}

const longRuntimeDir = 69

func longPrivateDir(t *testing.T, instance string) string {
	t.Helper()
	base := filepath.Join(tmpRoot, "e2e-"+instance)
	dir := filepath.Join(base, strings.Repeat("d", longRuntimeDir-len(base)-1))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(base); err != nil {
			t.Error(err)
		}
	})
	return dir
}

func entriesIn(t *testing.T, dir, instance string) []string {
	t.Helper()
	entries, err := daemonEntries(dir, instance)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range entries {
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return entries
}

func TestRemoveInstanceEntriesTakesTheHashedSocketsAndOnlyThem(t *testing.T) {
	instance, err := newInstanceID()
	if err != nil {
		t.Fatal(err)
	}
	dir := longPrivateDir(t, instance)
	ours := entriesIn(t, dir, instance)
	theirs := entriesIn(t, dir, "e2e-00000000")
	for _, socket := range ours[:2] {
		if !strings.HasPrefix(filepath.Base(socket), "hs-") {
			t.Fatalf("%s is not hashed, the directory is too short to test the hashed names", socket)
		}
	}
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
