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
