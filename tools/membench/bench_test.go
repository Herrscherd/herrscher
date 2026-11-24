package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"

	"github.com/Herrscherd/herrscher/core/clitext"
)

func TestMain(m *testing.M) {
	if os.Getenv(fakeEnv) == "1" {
		os.Exit(fakeHerrscher(os.Args[1:], os.Stdout, os.Stderr, os.Getenv))
	}
	os.Exit(m.Run())
}

func fakeBinary(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake herrscher is a shell wrapper")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "herrscher")
	script := fmt.Sprintf("#!/bin/sh\n%s=1 exec '%s' \"$@\"\n", fakeEnv, exe)
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func runFake(t *testing.T, extra ...string) (report, exitCode) {
	t.Helper()
	t.Setenv("TMPDIR", t.TempDir())
	args := append([]string{"-herrscher", fakeBinary(t), "-format", "json"}, extra...)
	var stdout, stderr bytes.Buffer
	code := run(args, &stdout, &stderr, func(string) string { return "" })
	if code == exitUsage || code == exitFailed {
		t.Fatalf("membench exited %d: %s", code, stderr.String())
	}
	var rep report
	if err := json.Unmarshal(stdout.Bytes(), &rep); err != nil {
		t.Fatalf("report is not JSON: %v\n%s", err, stdout.String())
	}
	return rep, code
}

func rowFor(t *testing.T, rep report, group clitext.Key) row {
	t.Helper()
	for i := range rep.Rows {
		if rep.Rows[i].Group == group {
			return rep.Rows[i]
		}
	}
	t.Fatalf("no row for %s", group)
	return row{}
}

func TestBenchMeasuresBothArms(t *testing.T) {
	rep, code := runFake(t, "-budget-tokens", "1000000", "-settings", "raw-archive")
	if code != exitComplete || rep.Status != statusComplete || rep.NotRun != 0 {
		t.Fatalf("code %d, status %q, not run %d; want a complete run", code, rep.Status, rep.NotRun)
	}
	cases := []struct {
		group        clitext.Key
		offSuccesses int
		onSuccesses  int
	}{
		{clitext.MembenchVariantSameAgent, 0, 1},
		{clitext.MembenchVariantOtherAgent, 0, 1},
		{clitext.MembenchVariantStale, 0, 1},
		{clitext.MembenchVariantNeverGiven, 1, 1},
	}
	for _, tc := range cases {
		r := rowFor(t, rep, tc.group)
		if r.Off.Successes != tc.offSuccesses || r.On.Successes != tc.onSuccesses {
			t.Errorf("%s: off %d, on %d successes; want off %d, on %d", tc.group, r.Off.Successes, r.On.Successes, tc.offSuccesses, tc.onSuccesses)
		}
		if r.On.FalseRecalls != 0 || r.Off.FalseRecalls != 0 {
			t.Errorf("%s: false recalls off %d, on %d; want none", tc.group, r.Off.FalseRecalls, r.On.FalseRecalls)
		}
		if r.MemoryTokensPerTurn <= 0 {
			t.Errorf("%s: memory tokens per turn %v; want above zero when the archive stores facts", tc.group, r.MemoryTokensPerTurn)
		}
	}
}

func TestBenchStopsAtBudgetCeiling(t *testing.T) {
	const limit = 400
	rep, code := runFake(t, "-budget-tokens", strconv.Itoa(limit))
	if code != exitPartial || rep.Status != statusPartial {
		t.Fatalf("code %d, status %q; want a partial stop", code, rep.Status)
	}
	if rep.NotRun == 0 {
		t.Errorf("a stopped run must report the arms it did not run")
	}
	if rep.SpentTokens > limit {
		t.Errorf("spent %d tokens above the ceiling %d", rep.SpentTokens, limit)
	}
}
