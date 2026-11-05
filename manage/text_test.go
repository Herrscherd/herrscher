package manage

import (
	"testing"

	"github.com/Herrscherd/herrscher/core/language"
)

func TestManageTextRendersInFrench(t *testing.T) {
	t.Setenv(language.EnvVar, "fr")

	if got, want := changeLine(verbAdd, "mod/a", "plugins.go"), "ajouté mod/a dans plugins.go"; got != want {
		t.Fatalf("changeLine(add) = %q, want %q", got, want)
	}
	if got, want := changeLine("remove", "mod/a", "plugins.go"), "retiré mod/a de plugins.go"; got != want {
		t.Fatalf("changeLine(remove) = %q, want %q", got, want)
	}
	if got, want := stateWord(verbAdd), "ajouté"; got != want {
		t.Fatalf("stateWord(add) = %q, want %q", got, want)
	}
	if got, want := stateWord("remove"), "retiré"; got != want {
		t.Fatalf("stateWord(remove) = %q, want %q", got, want)
	}
}
