package manager

import (
	"context"
	"testing"
)

func TestSetHomeCategory(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "category")
	if _, err := h.setHomeRun(context.Background(), args("channel", "cat1")); err != nil {
		t.Fatal(err)
	}
	if st.Home.ID != "cat1" || st.Home.Type != "category" {
		t.Fatalf("home not persisted: %+v", st.Home)
	}
}

func TestSetHomeRejectsPlainChannel(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "text")
	if _, err := h.setHomeRun(context.Background(), args("channel", "c9")); err == nil {
		t.Fatal("expected a plain text channel to be rejected as home")
	}
	if st.Home.ID != "" {
		t.Fatalf("rejected home must not persist: %+v", st.Home)
	}
}

func TestSetHomeMissingChannel(t *testing.T) {
	h, _, _, _, _, _ := newTestHandler(t, "category")
	if _, err := h.setHomeRun(context.Background(), args()); err == nil {
		t.Fatal("expected missing channel to error")
	}
}

func TestSetSource(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "category")
	if _, err := h.setSourceRun(context.Background(), args("path", "/src/herrscher")); err != nil {
		t.Fatal(err)
	}
	if st.SourceDir() != "/src/herrscher" {
		t.Fatalf("source not persisted: %q", st.SourceDir())
	}
}
