package state

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestCompareAndSetReviewPositionClaimsOnlyFromTheHeldPosition(t *testing.T) {
	s := NewState(filepath.Join(t.TempDir(), "state.json"))
	must(t, s.AddSession(Session{Name: "main"}))
	held := ReviewPosition{}
	claimed := ReviewPosition{Number: 7, Seen: map[string]int{"t1": 1}}

	ok, err := s.CompareAndSetReviewPosition("main", held, claimed)
	must(t, err)
	if !ok {
		t.Fatal("first claim refused, want accepted")
	}
	ok, err = s.CompareAndSetReviewPosition("main", held, ReviewPosition{Number: 7, Seen: map[string]int{"t1": 2}})
	must(t, err)
	if ok {
		t.Fatal("second claim from a stale position accepted, want refused")
	}
	if got, _ := s.FindSession("main"); !got.ReviewPosition().Equal(claimed) {
		t.Fatalf("position = %+v, want %+v", got.ReviewPosition(), claimed)
	}
}

func TestReviewPositionSurvivesReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := NewState(path)
	must(t, s.AddSession(Session{Name: "main"}))
	position := ReviewPosition{Number: 7, Seen: map[string]int{"t1": 3, "t2": 1}}
	_, err := s.CompareAndSetReviewPosition("main", ReviewPosition{}, position)
	must(t, err)

	reloaded, err := LoadState(path)
	must(t, err)
	got, _ := reloaded.FindSession("main")
	if !got.ReviewPosition().Equal(position) {
		t.Fatalf("reloaded position = %+v, want %+v", got.ReviewPosition(), position)
	}
}

func TestCompareAndSetReviewPositionRollsBackWhenSaveFails(t *testing.T) {
	s := NewState(filepath.Join(t.TempDir(), "state.json"))
	must(t, s.AddSession(Session{Name: "main"}))
	s.write = failEveryWrite

	ok, err := s.CompareAndSetReviewPosition("main", ReviewPosition{}, ReviewPosition{Number: 7, Seen: map[string]int{"t1": 1}})
	if !errors.Is(err, errInjectedSave) || ok {
		t.Fatalf("claim = %v, %v, want the save failure and no claim", ok, err)
	}
	if got, _ := s.FindSession("main"); !got.ReviewPosition().Equal(ReviewPosition{}) {
		t.Fatalf("memory after a failed claim = %+v, want the empty position", got.ReviewPosition())
	}
}

func TestReviewPositionOfAnotherRequestStartsEmpty(t *testing.T) {
	position := ReviewPosition{Number: 7, Seen: map[string]int{"t1": 3}}

	if got := position.SeenOf(8); got != nil {
		t.Fatalf("SeenOf(8) = %v, want nil for another request", got)
	}
	if got := position.SeenOf(7); got["t1"] != 3 {
		t.Fatalf("SeenOf(7) = %v, want the recorded counts", got)
	}
}
