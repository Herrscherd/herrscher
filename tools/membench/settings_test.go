package main

import (
	"slices"
	"testing"
)

func TestSelectSettingsAllAndNamed(t *testing.T) {
	all, err := selectSettings(settingsAll)
	if err != nil || len(all) != len(knownSettings()) {
		t.Fatalf("all settings: %v, %+v", err, all)
	}
	chosen, err := selectSettings("raw-archive, learned-skills")
	if err != nil {
		t.Fatal(err)
	}
	names := []string{chosen[0].name, chosen[1].name}
	if !slices.Equal(names, []string{"raw-archive", "learned-skills"}) {
		t.Errorf("selected %v, want raw-archive then learned-skills", names)
	}
}

func TestSelectSettingsRejectsAnUnknownName(t *testing.T) {
	if _, err := selectSettings("raw-archive,nope"); err == nil {
		t.Error("an unknown setting name was accepted")
	}
}

func TestAssignmentWritesTheEnvironmentValue(t *testing.T) {
	raw, ok := settingNamed("raw-archive")
	if !ok {
		t.Fatal("raw-archive is not a known setting")
	}
	if got := raw.assignment(true); got != "MEMORY_RAW_ARCHIVE=true" {
		t.Errorf("on assignment %q", got)
	}
	if got := raw.assignment(false); got != "MEMORY_RAW_ARCHIVE=false" {
		t.Errorf("off assignment %q", got)
	}
}

func TestEveryKnownSettingHasItsOwnVariable(t *testing.T) {
	seen := make(map[string]bool)
	for _, known := range knownSettings() {
		if seen[known.env] {
			t.Errorf("two settings share the variable %s", known.env)
		}
		seen[known.env] = true
	}
}
