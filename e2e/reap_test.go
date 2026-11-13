//go:build e2e && linux

package e2e_test

import (
	"errors"
	"os/exec"
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
