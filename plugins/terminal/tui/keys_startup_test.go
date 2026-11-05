package tui

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestTableErrorAtStartupStaysInTheKeyProblems(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newModel(&fakeBackend{})
	m.buildTables = func() (*dispatchTables, error) {
		return &dispatchTables{}, errors.New("doublon")
	}
	m.loadKeys(t.TempDir())
	if !strings.HasPrefix(m.flash, "inconsistent shortcuts") {
		t.Fatalf("a table error must be announced at startup, got flash %q", m.flash)
	}
	m.flash = "autre message"
	if !slices.ContainsFunc(m.keyProblems, func(problem string) bool {
		return strings.HasPrefix(problem, "inconsistent shortcuts")
	}) {
		t.Fatalf("a table error must stay recorded after the flash is overwritten, got %v", m.keyProblems)
	}
}
