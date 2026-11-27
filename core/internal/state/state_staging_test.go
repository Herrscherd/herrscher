package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveWritesPrivatelyPastAStrayStagingName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := NewState(path)
	if err := s.AddSession(Session{Name: "alpha"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path+".tmp", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); err != nil {
		t.Fatalf("Save blocked by a stray staging name: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != privateFileMode {
		t.Fatalf("state mode = %v, %v; want %v", info.Mode().Perm(), err, privateFileMode)
	}
	reloaded, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.FindSession("alpha"); !ok {
		t.Fatal("the saved session is missing after reload")
	}
}
