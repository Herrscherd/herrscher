package state

import (
	"path/filepath"
	"testing"
)

func TestSetCompactPercentPersistsAndClears(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st := NewState(path)
	st.Sessions = append(st.Sessions, Session{Name: "alpha", Vendor: "claude"})
	zero := 0

	if ok, err := st.SetCompactPercent("alpha", &zero); err != nil || !ok {
		t.Fatalf("SetCompactPercent(0) = %v, %v; want a match", ok, err)
	}
	reloaded, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	got, ok := reloaded.FindSession("alpha")
	if !ok || got.CompactPercent == nil || *got.CompactPercent != 0 {
		t.Fatalf("persisted CompactPercent = %v, want a pointer to 0", got.CompactPercent)
	}

	if ok, err := st.SetCompactPercent("alpha", nil); err != nil || !ok {
		t.Fatalf("SetCompactPercent(nil) = %v, %v; want a match", ok, err)
	}
	if cleared, _ := st.FindSession("alpha"); cleared.CompactPercent != nil {
		t.Fatal("a nil percent must drop the override so the config default applies")
	}
}

func TestSetCompactPercentUnknownSession(t *testing.T) {
	st := NewState(filepath.Join(t.TempDir(), "state.json"))
	ninety := 90
	ok, err := st.SetCompactPercent("ghost", &ninety)
	if err != nil {
		t.Fatalf("SetCompactPercent: %v", err)
	}
	if ok {
		t.Fatal("an unknown session must not match")
	}
}
