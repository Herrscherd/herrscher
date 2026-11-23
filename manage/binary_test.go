package manage

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func lockedImages(paths ...string) removeFunc {
	return func(path string) error {
		for _, locked := range paths {
			if path == locked || strings.HasPrefix(filepath.Base(path), filepath.Base(locked)+parkedInfix) {
				return &fs.PathError{Op: "remove", Path: path, Err: fs.ErrPermission}
			}
		}
		return os.Remove(path)
	}
}

func parkedBinaries(t *testing.T, target string) []string {
	t.Helper()
	matches, err := filepath.Glob(staleBinary(target) + parkedInfix + "*")
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

func writeBinaries(t *testing.T, files map[string]string) {
	t.Helper()
	for path, body := range files {
		if err := os.WriteFile(path, []byte(body), modeFile); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSwapBinaryParksAPreviousBinaryStillRunning(t *testing.T) {
	target := filepath.Join(t.TempDir(), binaryName)
	writeBinaries(t, map[string]string{target: "current", staleBinary(target): "running", freshBinary(target): "new"})
	if err := swapBinaryWith(target, lockedImages(staleBinary(target))); err != nil {
		t.Fatalf("swap with a running previous binary: %v", err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "new" {
		t.Fatalf("target = %q (%v), want the new build", data, err)
	}
	if data, err := os.ReadFile(staleBinary(target)); err != nil || string(data) != "current" {
		t.Fatalf("stale = %q (%v), want the binary just replaced", data, err)
	}
	parked := parkedBinaries(t, target)
	if len(parked) != 1 {
		t.Fatalf("parked = %v, want the running binary moved aside once", parked)
	}
	if data, err := os.ReadFile(parked[0]); err != nil || string(data) != "running" {
		t.Fatalf("parked = %q (%v), want the running binary", data, err)
	}
}

func TestSwapBinarySweepsParkedBinariesOnceTheyStop(t *testing.T) {
	target := filepath.Join(t.TempDir(), binaryName)
	stillRunning, stopped := staleBinary(target)+parkedInfix+"1", staleBinary(target)+parkedInfix+"2"
	writeBinaries(t, map[string]string{target: "current", freshBinary(target): "new", stillRunning: "a", stopped: "b"})
	remove := func(path string) error {
		if path == stillRunning {
			return &fs.PathError{Op: "remove", Path: path, Err: fs.ErrPermission}
		}
		return os.Remove(path)
	}
	if err := swapBinaryWith(target, remove); err != nil {
		t.Fatalf("swap over a locked parked binary: %v", err)
	}
	parked := parkedBinaries(t, target)
	if len(parked) != 1 || parked[0] != stillRunning {
		t.Fatalf("parked = %v, want only the one still running left", parked)
	}
}

func TestSwapBinaryReportsARemoveFailureThatIsNotALock(t *testing.T) {
	target := filepath.Join(t.TempDir(), binaryName)
	writeBinaries(t, map[string]string{target: "current", staleBinary(target): "old", freshBinary(target): "new"})
	broken := errors.New("disk gone")
	remove := func(string) error { return broken }
	if err := swapBinaryWith(target, remove); !errors.Is(err, broken) {
		t.Fatalf("err = %v, want the remove failure reported", err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "current" {
		t.Fatalf("target = %q (%v), want it untouched", data, err)
	}
}

func TestOnlyAGoBuildTempDirIsAGoRunBuild(t *testing.T) {
	cases := map[string]bool{
		filepath.Join(os.TempDir(), "go-build1234", "b001", "exe", binaryName): true,
		filepath.Join("home", "u", "go-build-tools", binaryName):               false,
		filepath.Join("home", "u", "go-build", binaryName):                     false,
		filepath.Join("usr", "local", "bin", binaryName):                       false,
	}
	for path, want := range cases {
		if got := builtByGoRun(path); got != want {
			t.Errorf("builtByGoRun(%q) = %v, want %v", path, got, want)
		}
	}
}
