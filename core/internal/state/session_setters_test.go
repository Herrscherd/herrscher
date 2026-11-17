package state

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSetAccountRollsBackWhenSaveFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := NewState(path)
	must(t, s.AddSession(Session{Name: "main", Account: "a", ResumeToken: "tok", InterruptedTurn: "turn-1"}))
	s.write = failEveryWrite

	ok, err := s.SetAccount("main", "b", true)
	if !ok || !errors.Is(err, errInjectedSave) {
		t.Fatalf("SetAccount = %v, %v, want found and the save failure", ok, err)
	}
	if got, _ := s.FindSession("main"); got.Account != "a" || got.ResumeToken != "tok" || got.InterruptedTurn != "turn-1" {
		t.Fatalf("memory after a failed SetAccount = account %q, token %q, mark %q, want a, tok, turn-1", got.Account, got.ResumeToken, got.InterruptedTurn)
	}
	reloaded, err := LoadState(path)
	must(t, err)
	if got, _ := reloaded.FindSession("main"); got.Account != "a" || got.ResumeToken != "tok" || got.InterruptedTurn != "turn-1" {
		t.Fatalf("file after a failed SetAccount = account %q, token %q, mark %q, want a, tok, turn-1", got.Account, got.ResumeToken, got.InterruptedTurn)
	}

	s.write = os.WriteFile
	must(t, s.SetHome(HomeRef{ID: "home", Type: "chat"}))
	reloaded, err = LoadState(path)
	must(t, err)
	if got, _ := reloaded.FindSession("main"); got.Account != "a" || got.ResumeToken != "tok" || got.InterruptedTurn != "turn-1" {
		t.Fatalf("file after a later save = account %q, token %q, mark %q, want the memory values a, tok, turn-1", got.Account, got.ResumeToken, got.InterruptedTurn)
	}
}

func TestSetBackendTargetRollsBackWhenSaveFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := NewState(path)
	must(t, s.AddSession(Session{Name: "main", Vendor: "v1", Cmd: "c1", ModelID: "m1", ResumeToken: "tok", InterruptedTurn: "turn-1"}))
	s.write = failEveryWrite

	ok, err := s.SetBackendTarget("main", "v2", "c2", "m2")
	if !ok || !errors.Is(err, errInjectedSave) {
		t.Fatalf("SetBackendTarget = %v, %v, want found and the save failure", ok, err)
	}
	want := Session{Vendor: "v1", Cmd: "c1", ModelID: "m1", ResumeToken: "tok", InterruptedTurn: "turn-1"}
	if got, _ := s.FindSession("main"); got.Vendor != want.Vendor || got.Cmd != want.Cmd || got.ModelID != want.ModelID || got.ResumeToken != want.ResumeToken || got.InterruptedTurn != want.InterruptedTurn {
		t.Fatalf("memory after a failed SetBackendTarget = vendor %q, cmd %q, model %q, token %q, mark %q, want v1, c1, m1, tok, turn-1", got.Vendor, got.Cmd, got.ModelID, got.ResumeToken, got.InterruptedTurn)
	}
	reloaded, err := LoadState(path)
	must(t, err)
	if got, _ := reloaded.FindSession("main"); got.Vendor != want.Vendor || got.Cmd != want.Cmd || got.ModelID != want.ModelID || got.ResumeToken != want.ResumeToken || got.InterruptedTurn != want.InterruptedTurn {
		t.Fatalf("file after a failed SetBackendTarget = vendor %q, cmd %q, model %q, token %q, mark %q, want v1, c1, m1, tok, turn-1", got.Vendor, got.Cmd, got.ModelID, got.ResumeToken, got.InterruptedTurn)
	}

	s.write = os.WriteFile
	must(t, s.SetHome(HomeRef{ID: "home", Type: "chat"}))
	reloaded, err = LoadState(path)
	must(t, err)
	if got, _ := reloaded.FindSession("main"); got.Vendor != want.Vendor || got.Cmd != want.Cmd || got.ModelID != want.ModelID || got.ResumeToken != want.ResumeToken || got.InterruptedTurn != want.InterruptedTurn {
		t.Fatalf("file after a later save = vendor %q, cmd %q, model %q, token %q, mark %q, want the memory values v1, c1, m1, tok, turn-1", got.Vendor, got.Cmd, got.ModelID, got.ResumeToken, got.InterruptedTurn)
	}
}

func TestZeroStateSetHomeWritesWithoutWriteSeam(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := &State{path: path}

	must(t, s.SetHome(HomeRef{ID: "home", Type: "chat"}))

	reloaded, err := LoadState(path)
	must(t, err)
	if reloaded.Home.ID != "home" || reloaded.Home.Type != "chat" {
		t.Fatalf("home on disk = %+v, want home/chat", reloaded.Home)
	}
}
