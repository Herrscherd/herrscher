package state

import (
	"path/filepath"
	"testing"
)

func TestSetSessionLanguagePersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := NewState(path)
	if err := s.AddSession(Session{Name: "main"}); err != nil {
		t.Fatal(err)
	}
	found, err := s.SetSessionLanguage("main", "fr")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("SetSessionLanguage reported no session for main")
	}
	reloaded, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := reloaded.FindSession("main")
	if got.Language != "fr" {
		t.Fatalf("reloaded language = %q, want fr", got.Language)
	}
}
