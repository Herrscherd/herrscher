package manage

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func startRunningImage(t *testing.T, path string) {
	t.Helper()
	ping, err := os.ReadFile(filepath.Join(os.Getenv("SystemRoot"), "System32", "PING.EXE"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, ping, modeFile); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), path, "-n", "60", "127.0.0.1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, killed := errors.AsType[*exec.ExitError](cmd.Wait()); !killed {
			t.Error("the running binary ended on its own before the swaps were done")
		}
	})
}

func TestSwapBinaryWhileEveryGenerationStillRunsOnWindows(t *testing.T) {
	target := filepath.Join(t.TempDir(), binaryName+windowsExt)
	startRunningImage(t, target)
	writeBinaries(t, map[string]string{freshBinary(target): "second"})
	if err := swapBinary(target); err != nil {
		t.Fatalf("first swap over a running binary: %v", err)
	}
	writeBinaries(t, map[string]string{freshBinary(target): "third"})
	if err := swapBinary(target); err != nil {
		t.Fatalf("second swap while the first generation still runs as .old: %v", err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "third" {
		t.Fatalf("target = %q (%v), want the last build", data, err)
	}
	if parked := parkedBinaries(t, target); len(parked) != 1 {
		t.Fatalf("parked = %v, want the running first generation moved aside", parked)
	}
}
