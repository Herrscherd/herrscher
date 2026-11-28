package state

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/reviewloop"
)

func TestCompareAndSetReviewPositionClaimsOnlyFromTheHeldPosition(t *testing.T) {
	s := NewState(filepath.Join(t.TempDir(), "state.json"))
	must(t, s.AddSession(Session{Name: "main"}))
	held := ReviewPosition{}
	claimed := ReviewPosition{Number: 7, Marks: map[string]reviewloop.Mark{"t1": {At: time.Unix(10, 0)}}}

	ok, err := s.CompareAndSetReviewPosition("main", held, claimed)
	must(t, err)
	if !ok {
		t.Fatal("first claim refused, want accepted")
	}
	ok, err = s.CompareAndSetReviewPosition("main", held, ReviewPosition{Number: 7, Legacy: map[string]int{"t1": 2}})
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
	at := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	position := ReviewPosition{
		Number:  7,
		Marks:   map[string]reviewloop.Mark{"t1": {At: at, Digests: []string{"d1"}}},
		Own:     []reviewloop.Own{{Thread: "t1", Digest: "d2", CreatedAt: at}},
		Pending: []reviewloop.Pending{{Thread: "t1", Digest: "d3", Login: "herrscher", PostedAt: at}},
	}
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

	ok, err := s.CompareAndSetReviewPosition("main", ReviewPosition{}, ReviewPosition{Number: 7, Legacy: map[string]int{"t1": 1}})
	if !errors.Is(err, errInjectedSave) || ok {
		t.Fatalf("claim = %v, %v, want the save failure and no claim", ok, err)
	}
	if got, _ := s.FindSession("main"); !got.ReviewPosition().Equal(ReviewPosition{}) {
		t.Fatalf("memory after a failed claim = %+v, want the empty position", got.ReviewPosition())
	}
}

func TestReviewPositionOfAnotherRequestStartsEmpty(t *testing.T) {
	position := ReviewPosition{Number: 7, Legacy: map[string]int{"t1": 3}, Pending: []reviewloop.Pending{{Thread: "t1"}}}

	other := position.ForRequest(8)
	if other.Number != 8 || other.Legacy != nil || other.Pending != nil || other.Marks != nil {
		t.Fatalf("ForRequest(8) = %+v, want an empty position of request 8", other)
	}
	if got := position.ForRequest(7); !got.Equal(position) {
		t.Fatalf("ForRequest(7) = %+v, want the recorded position", got)
	}
}
