package main

import (
	"slices"
	"testing"
)

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
	without := (&runner{}).assignments(option, true)
	if !slices.Equal(without, []string{"MEMORY_RAW_ARCHIVE=true"}) {
		t.Errorf("without a key: %v", without)
	}
	with := (&runner{apiKey: "placeholder-key"}).assignments(option, false)
	if !slices.Equal(with, []string{"MEMORY_RAW_ARCHIVE=false", "ANTHROPIC_API_KEY=placeholder-key"}) {
		t.Errorf("with a key: %v", with)
	}
}

func TestPlanArmsPairsEachSettingOffThenOn(t *testing.T) {
	plan := planArms(pairTasks()[:1], []setting{{name: "report", env: "MEMORY_REPORT_ENABLED"}})
	if len(plan) != armsPerSetting || plan[0].enabled || !plan[1].enabled {
		t.Errorf("plan %+v, want one off arm then one on arm", plan)
	}
}
