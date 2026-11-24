//go:build e2e && linux

package main

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const rawArchiveDir = "raw"

type binaries struct {
	herrscher string
}

func buildBinaries(tb testing.TB) binaries {
	tb.Helper()
	dir := tb.TempDir()
	herrscher := filepath.Join(dir, "herrscher")
	goBuild(tb, "../..", herrscher, "-tags", "e2e", ".")
	goBuild(tb, "../..", filepath.Join(dir, "claude"), "./e2e/vendortest/cmd/claude")
	tb.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return binaries{herrscher: herrscher}
}

func goBuild(tb testing.TB, dir, out string, args ...string) {
	tb.Helper()
	build := exec.CommandContext(tb.Context(), "go", append([]string{"build", "-o", out}, args...)...)
	build.Dir = dir
	build.Env = append(os.Environ(), "CGO_ENABLED=0", "GOWORK=off")
	if got, err := build.CombinedOutput(); err != nil {
		tb.Fatalf("go build %s: %v\n%s", out, err, got)
	}
}

func rawArchiveFiles(tb testing.TB, bins binaries, option setting, enabled bool) int {
	tb.Helper()
	box, err := newSandbox(tb.TempDir())
	if err != nil {
		tb.Fatal(err)
	}
	defer func() {
		if closeErr := box.close(); closeErr != nil {
			tb.Fatal(closeErr)
		}
	}()
	r := runner{binary: bins.herrscher, model: defaultModel}
	if _, err := r.invoke(tb.Context(), box, step{prompt: "Remember this: the code is 4242.", agent: agentAlpha}, []string{option.assignment(enabled)}); err != nil {
		tb.Fatalf("herrscher --print: %v", err)
	}
	archive := filepath.Join(box.home, ".herrscher", "memory", rawArchiveDir)
	count := 0
	walkErr := filepath.WalkDir(archive, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			count++
		}
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, fs.ErrNotExist) {
		tb.Fatal(walkErr)
	}
	return count
}

func TestMemorySettingReachesTheRealBinary(t *testing.T) {
	bins := buildBinaries(t)
	raw, ok := settingNamed("raw-archive")
	if !ok {
		t.Fatal("raw-archive setting is not known")
	}
	if got := rawArchiveFiles(t, bins, raw, false); got != 0 {
		t.Fatalf("raw archive written with MEMORY_RAW_ARCHIVE=false: %d files", got)
	}
	if got := rawArchiveFiles(t, bins, raw, true); got == 0 {
		t.Fatal("no raw archive with MEMORY_RAW_ARCHIVE=true: the setting was not forwarded to herrscher")
	}
}

func TestBenchMeasuresEachArmFromTheLedger(t *testing.T) {
	bins := buildBinaries(t)
	settings, err := selectSettings("raw-archive")
	if err != nil {
		t.Fatal(err)
	}
	tasks := pairTasks()[:1]
	r := runner{
		binary: bins.herrscher,
		model:  defaultModel,
		parent: t.TempDir(),
		meter:  &meter{limit: budget{tokens: 10000000}},
	}
	result, err := r.runAll(t.Context(), tasks, settings)
	if err != nil {
		t.Fatalf("runAll: %v", err)
	}
	if result.stopped {
		t.Fatal("execution stopped although the budget allows every arm")
	}
	if len(result.outcomes) != armsPerSetting {
		t.Fatalf("outcomes = %d, want %d", len(result.outcomes), armsPerSetting)
	}
	wantTurns := len(tasks[0].steps) + 1
	for _, got := range result.outcomes {
		if got.turns != wantTurns {
			t.Errorf("arm on=%t: turns = %d, want %d read from the ledger", got.on, got.turns, wantTurns)
		}
		if got.tokens <= 0 || got.input <= 0 {
			t.Errorf("arm on=%t: tokens = %d, input = %d, want the ledger's usage", got.on, got.tokens, got.input)
		}
	}
}

func TestBenchStopsWithoutAUsageLedger(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "herrscher")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\necho ok\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	settings, err := selectSettings("raw-archive")
	if err != nil {
		t.Fatal(err)
	}
	r := runner{
		binary: binary,
		model:  defaultModel,
		parent: t.TempDir(),
		meter:  &meter{limit: budget{tokens: 1000000}},
	}
	result, err := r.runAll(t.Context(), pairTasks()[:1], settings)
	var got messageError
	if !errors.As(err, &got) || got.key != msgErrNoUsage {
		t.Fatalf("runAll error = %v, want %s", err, msgErrNoUsage)
	}
	if !result.stopped {
		t.Fatal("execution is not marked stopped after the missing ledger")
	}
	if len(result.outcomes) != 0 {
		t.Fatalf("outcomes = %d, want none before the first arm fails", len(result.outcomes))
	}
}
