package manager

import (
	"context"
	"errors"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestCheckParentCycleRefusesAParentThatWouldLoop(t *testing.T) {
	all := []state.Session{
		{Name: "alpha"},
		{Name: "beta", Parent: "alpha"},
		{Name: "dangling", Parent: "fresh"},
	}
	for _, tc := range []struct {
		name, parent string
	}{
		{"alpha", "alpha"},
		{"alpha", "beta"},
		{"fresh", "dangling"},
	} {
		var invalid *InvalidValueError
		if err := checkParentCycle(tc.name, tc.parent, all); !errors.As(err, &invalid) {
			t.Fatalf("parent %q for %q: err = %v, want an InvalidValueError", tc.parent, tc.name, err)
		}
	}
}

func TestCheckParentCycleAcceptsAParentOutsideItsOwnTree(t *testing.T) {
	all := []state.Session{{Name: "alpha"}, {Name: "beta", Parent: "alpha"}}
	for _, tc := range []struct {
		name, parent string
	}{
		{"gamma", "alpha"},
		{"gamma", "beta"},
		{"gamma", "unknown"},
		{"gamma", ""},
	} {
		if err := checkParentCycle(tc.name, tc.parent, all); err != nil {
			t.Fatalf("parent %q for %q: %v, want no cycle", tc.parent, tc.name, err)
		}
	}
}

func TestSessionCreateRefusesAParentThatWouldLoop(t *testing.T) {
	h, _, _, wt, _, st := newTestHandler(t, "category")
	wt.path = t.TempDir()
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	st.Sessions = []state.Session{{Name: "dangling", Parent: "fresh"}}
	_, err := h.sessionCreateRun(context.Background(), args("name", "fresh", "parent", "dangling"))
	var invalid *InvalidValueError
	if !errors.As(err, &invalid) || invalid.Field != "parent" {
		t.Fatalf("err = %v, want an InvalidValueError on parent", err)
	}
	if _, exists := st.FindSession("fresh"); exists {
		t.Fatal("a refused parent must not leave the session created")
	}
}
