package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestHelpGivesEachKeyOneMeaning(t *testing.T) {
	help := ansi.Strip(newTestModel().helpView())
	meaning := make(map[string]string)
	for entry := range strings.SplitSeq(help, " · ") {
		key, label, found := strings.Cut(strings.TrimSpace(entry), " ")
		if !found {
			t.Fatalf("help entry %q has no label", entry)
		}
		if previous, seen := meaning[key]; seen {
			t.Fatalf("%s means both %q and %q", key, previous, label)
		}
		meaning[key] = label
	}
}

func TestEachLeaderKeyDoesADifferentThing(t *testing.T) {
	labels := make(map[string]string)
	for key, binding := range leaderBindings() {
		if other, seen := labels[binding.label]; seen {
			t.Fatalf("ctrl+x %s and ctrl+x %s share the label %q", key, other, binding.label)
		}
		labels[binding.label] = key
	}
}
