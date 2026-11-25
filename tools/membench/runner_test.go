package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/usage"
)

func TestEveryArmPlantsTheAgedNodeBeforeTheBinaryRuns(t *testing.T) {
	skipOffUnix(t)
	for _, enabled := range []bool{false, true} {
		t.Run("enabled="+strconv.FormatBool(enabled), func(t *testing.T) {
			seen, err := playArmCapturingVault(t, enabled)
			if seen != agedNodeText {
				t.Errorf("enabled=%t: the vault held %q when the binary ran, want the aged node", enabled, seen)
			}
			if known, ok := errors.AsType[messageError](err); !ok || known.key != msgErrNoUsage {
				t.Errorf("enabled=%t: error %v, want the missing-ledger error after the binary ran", enabled, err)
			}
		})
	}
}

func playArmCapturingVault(t *testing.T, enabled bool) (string, error) {
	t.Helper()
	dir := t.TempDir()
	capture := filepath.Join(dir, "seen.md")
	binary := filepath.Join(dir, "herrscher")
	script := "#!/bin/sh\ncat \"$OBSIDIAN_VAULT/" + agedNodeFile + "\" > '" + capture + "' || exit 3\n"
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	box, err := newSandbox(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bench := &runner{binary: binary, meter: &meter{}}
	planned := arm{current: pairTasks()[0], option: setting{name: "report", env: "MEMORY_REPORT_ENABLED"}, enabled: enabled}
	_, playErr := bench.play(context.Background(), box, planned)
	seen, readErr := os.ReadFile(capture)
	if readErr != nil {
		t.Fatal(readErr)
	}
	return string(seen), playErr
}

func TestRedactHidesTheKeyWhereItAppears(t *testing.T) {
	const secret = "placeholder-key"
	got := redact("auth failed for placeholder-key", secret)
	if got != "auth failed for [redacted]" {
		t.Errorf("redact = %q", got)
	}
	if redact("untouched", "") != "untouched" {
		t.Error("an empty secret changed the text")
	}
}

func TestAssignmentsCarryTheKeyOnlyWhenGiven(t *testing.T) {
	option := setting{name: "raw-archive", env: "MEMORY_RAW_ARCHIVE"}
	box := sandbox{root: "run"}
	vault := filepath.Join("run", "vault")
	without := (&runner{}).assignments(box, option, true)
	if !slices.Equal(without, []string{"MEMORY_RAW_ARCHIVE=true", "TERMINAL_CONSOLIDATE_EVERY=0", "OBSIDIAN_VAULT=" + vault}) {
		t.Errorf("without a key: %v", without)
	}
	with := (&runner{apiKey: "placeholder-key"}).assignments(box, option, false)
	if !slices.Equal(with, []string{"MEMORY_RAW_ARCHIVE=false", "TERMINAL_CONSOLIDATE_EVERY=0", "OBSIDIAN_VAULT=" + vault, "ANTHROPIC_API_KEY=placeholder-key"}) {
		t.Errorf("with a key: %v", with)
	}
}

func TestEveryArmSetsItsSettingAndConsolidatesOnEveryRun(t *testing.T) {
	box := sandbox{root: "run"}
	for _, option := range knownSettings() {
		for _, enabled := range []bool{false, true} {
			got := (&runner{}).assignments(box, option, enabled)
			if !slices.Contains(got, option.assignment(enabled)) {
				t.Errorf("arm %s enabled=%t: %v lacks its setting", option.name, enabled, got)
			}
			if !slices.Contains(got, "TERMINAL_CONSOLIDATE_EVERY=0") {
				t.Errorf("arm %s enabled=%t: %v does not consolidate on every run, so report and sweep never act", option.name, enabled, got)
			}
		}
	}
}

func TestPlanArmsPairsEachSettingOffThenOn(t *testing.T) {
	plan := planArms(pairTasks()[:1], []setting{{name: "report", env: "MEMORY_REPORT_ENABLED"}})
	if len(plan) != armsPerSetting || plan[0].enabled || !plan[1].enabled {
		t.Errorf("plan %+v, want one off arm then one on arm", plan)
	}
}

func TestFailedCallIsChargedFromTheLedger(t *testing.T) {
	skipOffUnix(t)
	binary := filepath.Join(t.TempDir(), "herrscher")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	box, err := newSandbox(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ledger := usage.Ledger{Dir: filepath.Join(box.state, usageSubdir), Now: time.Now}
	if err = ledger.Append([]usage.Record{{Time: time.Now(), In: 100, Out: 20, Cost: 0.01}}); err != nil {
		t.Fatal(err)
	}
	bench := &runner{binary: binary, meter: &meter{}}
	planned := arm{current: pairTasks()[0], option: setting{name: "report", env: "MEMORY_REPORT_ENABLED"}, enabled: true}
	if _, err = bench.play(context.Background(), box, planned); err == nil {
		t.Fatal("a failing binary returned no error")
	}
	if bench.meter.spent.tokens != 120 {
		t.Errorf("spent %d tokens, want the 120 the ledger recorded for the failed call", bench.meter.spent.tokens)
	}
}

func TestUnreadableLedgerDayStillChargesTheReadableSpend(t *testing.T) {
	skipOffUnix(t)
	binary := filepath.Join(t.TempDir(), "herrscher")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	box, err := newSandbox(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(box.state, usageSubdir)
	ledger := usage.Ledger{Dir: dir, Now: time.Now}
	if err = ledger.Append([]usage.Record{{Time: time.Now(), In: 100, Out: 20, Cost: 0.01}}); err != nil {
		t.Fatal(err)
	}
	unreadable := time.Now().Add(-24*time.Hour).UTC().Format(time.DateOnly) + ".jsonl"
	if err = os.MkdirAll(filepath.Join(dir, unreadable), 0o700); err != nil {
		t.Fatal(err)
	}
	bench := &runner{binary: binary, meter: &meter{}}
	planned := arm{current: pairTasks()[0], option: setting{name: "report", env: "MEMORY_REPORT_ENABLED"}, enabled: true}
	_, err = bench.play(context.Background(), box, planned)
	if known, ok := errors.AsType[messageError](err); !ok || known.key != msgErrSpendPartial {
		t.Fatalf("error %v, want the spend_partial message for the unreadable day", err)
	}
	if bench.meter.spent.tokens != 120 {
		t.Errorf("spent %d tokens, want the 120 the readable day recorded", bench.meter.spent.tokens)
	}
}

func TestOversizedPromptIsRefusedBeforeTheBinaryRuns(t *testing.T) {
	bench := &runner{binary: filepath.Join(t.TempDir(), "missing"), meter: &meter{}}
	box, err := newSandbox(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	request := step{agent: agentAlpha, prompt: strings.Repeat("x", maxPromptBytes+1)}
	_, _, err = bench.call(context.Background(), box, task{id: "long"}, request, nil)
	known, ok := errors.AsType[messageError](err)
	if !ok || known.key != msgErrPromptLong {
		t.Errorf("error %v, want the prompt-too-long message", err)
	}
	if known.params[paramLimit] != byteSize(maxPromptBytes) {
		t.Errorf("limit %v, want the cap as a byte size", known.params[paramLimit])
	}
}
