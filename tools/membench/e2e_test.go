//go:build e2e && linux

package main

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func printIntoVault(tb testing.TB, bins binaries, option setting, enabled bool, prompt string, count func(vault string) (int, error)) int {
	tb.Helper()
	box, err := newSandbox(tb.Context(), tb.TempDir())
	if err != nil {
		tb.Fatal(err)
	}
	defer func() {
		if closeErr := box.close(); closeErr != nil {
			tb.Fatal(closeErr)
		}
	}()
	if plantErr := box.plantAgedNode(); plantErr != nil {
		tb.Fatal(plantErr)
	}
	r := runner{binary: bins.herrscher, model: defaultModel}
	if _, invokeErr := r.invoke(tb.Context(), box, step{prompt: prompt, agent: agentAlpha}, r.assignments(box, option, enabled)); invokeErr != nil {
		tb.Fatalf("herrscher --print: %v", invokeErr)
	}
	found, err := count(box.vault())
	if err != nil {
		tb.Fatal(err)
	}
	return found
}

func countFiles(root string, keep func(data []byte) bool) (int, error) {
	count := 0
	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, readErr := os.ReadFile(filepath.Clean(path))
		if readErr != nil {
			return readErr
		}
		if keep(data) {
			count++
		}
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, fs.ErrNotExist) {
		return 0, walkErr
	}
	return count, nil
}

func rawArchiveFiles(tb testing.TB, bins binaries, option setting, enabled bool) int {
	tb.Helper()
	return printIntoVault(tb, bins, option, enabled, "Remember this: the code is 4242.", func(vault string) (int, error) {
		return countFiles(filepath.Join(vault, rawArchiveDir), func([]byte) bool { return true })
	})
}

func learnedSkillFiles(tb testing.TB, bins binaries, option setting, enabled bool) int {
	tb.Helper()
	prompt := `Here is what I learned: <skill name="quorr-gate">Open the Quorr gate with code 4242.</skill>`
	return printIntoVault(tb, bins, option, enabled, prompt, func(vault string) (int, error) {
		return countFiles(vault, func(data []byte) bool {
			return strings.Contains(string(data), "type: skill") && strings.Contains(string(data), "quorr-gate")
		})
	})
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

func reportFiles(tb testing.TB, bins binaries, option setting, enabled bool) int {
	tb.Helper()
	return printIntoVault(tb, bins, option, enabled, "Remember this: the code is 4242.", func(vault string) (int, error) {
		aged, err := os.ReadFile(filepath.Join(vault, agedNodeFile))
		if err != nil {
			return 0, err
		}
		if !strings.Contains(string(aged), "state: archived") {
			return 0, errors.New("the sweep did not archive the aged node")
		}
		return countFiles(vault, func(data []byte) bool {
			return strings.Contains(string(data), "type: report")
		})
	})
}

func TestReportSettingReachesTheRealBinary(t *testing.T) {
	bins := buildBinaries(t)
	report, ok := settingNamed("report")
	if !ok {
		t.Fatal("report setting is not known")
	}
	if got := reportFiles(t, bins, report, false); got != 0 {
		t.Fatalf("report written with MEMORY_REPORT_ENABLED=false: %d files", got)
	}
	if got := reportFiles(t, bins, report, true); got == 0 {
		t.Fatal("no report with MEMORY_REPORT_ENABLED=true: the setting was not forwarded to herrscher")
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
	arms := planArms(tasks, settings)
	if len(arms) != armsPerSetting {
		t.Fatalf("arms = %d, want %d", len(arms), armsPerSetting)
	}
	wantTurns := len(tasks[0].steps) + 1
	for _, planned := range arms {
		got, ledger := playArmWithLedger(t, r, planned)
		checkArmAgainstLedger(t, planned, got, ledger, wantTurns)
	}
}

func playArmWithLedger(t *testing.T, r runner, planned arm) (outcome, tally) {
	t.Helper()
	box, err := newSandbox(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.play(t.Context(), box, planned)
	if err != nil {
		t.Fatal(errors.Join(err, box.close()))
	}
	ledger, err := readTally(box.state)
	if err != nil {
		t.Fatal(errors.Join(err, box.close()))
	}
	if closeErr := box.close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	return got, ledger
}

func checkArmAgainstLedger(t *testing.T, planned arm, got outcome, ledger tally, wantTurns int) {
	t.Helper()
	if ledger.turns != wantTurns {
		t.Errorf("arm on=%t: ledger turns = %d, want %d", planned.enabled, ledger.turns, wantTurns)
	}
	if got.turns != ledger.turns || got.tokens != ledger.tokens || got.input != ledger.input {
		t.Errorf("arm on=%t: outcome turns=%d tokens=%d input=%d, ledger turns=%d tokens=%d input=%d",
			planned.enabled, got.turns, got.tokens, got.input, ledger.turns, ledger.tokens, ledger.input)
	}
	if ledger.input <= 0 {
		t.Errorf("arm on=%t: ledger input = %d, want the vendor's reported usage", planned.enabled, ledger.input)
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

func TestLearnedSkillSettingReachesTheRealBinary(t *testing.T) {
	bins := buildBinaries(t)
	learned, ok := settingNamed("learned-skills")
	if !ok {
		t.Fatal("learned-skills setting is not known")
	}
	if got := learnedSkillFiles(t, bins, learned, false); got != 0 {
		t.Fatalf("skill written with MEMORY_LEARNED_SKILLS=false: %d files", got)
	}
	if got := learnedSkillFiles(t, bins, learned, true); got == 0 {
		t.Fatal("no skill with MEMORY_LEARNED_SKILLS=true: the setting was not forwarded to herrscher")
	}
}
