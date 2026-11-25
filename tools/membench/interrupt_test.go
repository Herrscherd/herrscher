package main

import (
	"context"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
)

func TestInterruptedRunIsNotReportedAsBudget(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	msgs := loadCatalogue()
	cfg, err := parseConfig(budgetArgs("-herrscher", fakeBinary(t)), nil, msgs, i18n.English, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	tasks, _, err := loadTasks(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	rep, runErr := execute(ctx, cfg, tasks, 0, msgs)
	if runErr != nil {
		t.Fatalf("execute returned %v for an interrupted run, want the report alone", runErr)
	}
	if rep.Status != statusInterrupted {
		t.Fatalf("status %q, want %q", rep.Status, statusInterrupted)
	}
	if rep.NotRun != plannedArms(cfg, tasks) {
		t.Errorf("NotRun %d, want the %d planned arms", rep.NotRun, plannedArms(cfg, tasks))
	}
	var out strings.Builder
	if err := writeReport(&out, rep, cfg, msgs); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "budget") {
		t.Errorf("report %q blames the budget for a Ctrl-C", out.String())
	}
	if !strings.Contains(out.String(), "interrupted by a signal") {
		t.Errorf("report %q does not say the run was interrupted", out.String())
	}
}
