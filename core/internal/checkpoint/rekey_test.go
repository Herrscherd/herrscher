package checkpoint

import (
	"errors"
	"strings"
	"testing"
)

const rekeyTarget = "inc_TARGET"

func checkpointsUnder(t *testing.T, store *Store, dir, key string) []Entry {
	t.Helper()
	entries, err := store.List(t.Context(), dir, key)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func rekey(t *testing.T, store *Store, dir, from, to string) Rekey {
	t.Helper()
	plan, err := PlanRekey(t.Context(), dir, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyRekey(t.Context(), plan); err != nil {
		t.Fatal(err)
	}
	return plan
}

func takeTwoAndRewind(t *testing.T, store *Store, dir, key string) []Entry {
	t.Helper()
	for _, text := range []string{"un", "deux"} {
		write(t, dir, "main.go", "package main // "+text+"\n")
		if _, err := store.Take(t.Context(), dir, key, Meta{Turn: text, Text: text}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.Restore(t.Context(), dir, key, 1); err != nil {
		t.Fatal(err)
	}
	return checkpointsUnder(t, store, dir, key)
}

func TestRekeyMovesTheCheckpointsAndTheUndoPointOnce(t *testing.T) {
	var store Store
	dir := initRepo(t)
	before := takeTwoAndRewind(t, &store, dir, "alpha")

	if plan := rekey(t, &store, dir, "alpha", rekeyTarget); len(plan.Moves) != 3 {
		t.Fatalf("plan moves %+v, want the two checkpoints and the undo point", plan.Moves)
	}
	if left := checkpointsUnder(t, &store, dir, "alpha"); len(left) != 0 {
		t.Fatalf("the old key keeps %+v", left)
	}
	after := checkpointsUnder(t, &store, dir, rekeyTarget)
	if len(after) != len(before) || after[0].Commit != before[0].Commit || after[1].Commit != before[1].Commit {
		t.Fatalf("checkpoints under the new key = %+v, want %+v", after, before)
	}
	if store.HasUndo(t.Context(), dir, "alpha") || !store.HasUndo(t.Context(), dir, rekeyTarget) {
		t.Fatal("the undo point did not move to the new key")
	}
	if again := rekey(t, &store, dir, "alpha", rekeyTarget); !again.Empty() {
		t.Fatalf("a second rekey plans %+v, want nothing", again.Moves)
	}
}

func TestRekeyFinishesARefAlreadyAtItsNewName(t *testing.T) {
	var store Store
	dir := initRepo(t)
	before := takeTwoAndRewind(t, &store, dir, "alpha")
	run(t, dir, "update-ref", checkpointRef(rekeyTarget, 1), before[0].Commit)

	rekey(t, &store, dir, "alpha", rekeyTarget)
	if left := checkpointsUnder(t, &store, dir, "alpha"); len(left) != 0 {
		t.Fatalf("the old key keeps %+v", left)
	}
	if after := checkpointsUnder(t, &store, dir, rekeyTarget); len(after) != 2 {
		t.Fatalf("checkpoints under the new key = %+v, want both", after)
	}
}

func TestRekeyRefusesATargetHoldingAnotherCommitAndMovesNothing(t *testing.T) {
	var store Store
	dir := initRepo(t)
	before := takeTwoAndRewind(t, &store, dir, "alpha")
	run(t, dir, "update-ref", checkpointRef(rekeyTarget, 2), before[0].Commit)

	_, err := PlanRekey(t.Context(), dir, "alpha", rekeyTarget)
	conflict, ok := errors.AsType[*RekeyConflictError](err)
	if !ok || !errors.Is(err, ErrRekeyConflict) {
		t.Fatalf("err = %v, want a %v", err, ErrRekeyConflict)
	}
	if conflict.Ref != checkpointRef(rekeyTarget, 2) || conflict.Have != before[0].Commit || conflict.Want != before[1].Commit {
		t.Fatalf("conflict = %+v, want ref 2 holding checkpoint 1 instead of checkpoint 2", conflict)
	}
	if left := checkpointsUnder(t, &store, dir, "alpha"); len(left) != 2 {
		t.Fatalf("the refused rekey moved refs: old key keeps %+v", left)
	}
}

func TestRekeyLeavesAKeyThatOnlySharesThePrefixAlone(t *testing.T) {
	var store Store
	dir := initRepo(t)
	takeTwoAndRewind(t, &store, dir, "alpha")
	other := takeTwoAndRewind(t, &store, dir, "alpha2")

	for _, move := range rekey(t, &store, dir, "alpha", rekeyTarget).Moves {
		if strings.Contains(move.From, "alpha2") {
			t.Fatalf("rekey of alpha moved %s", move.From)
		}
	}
	if kept := checkpointsUnder(t, &store, dir, "alpha2"); len(kept) != len(other) || !store.HasUndo(t.Context(), dir, "alpha2") {
		t.Fatalf("alpha2 keeps %+v, want its own checkpoints and undo point", kept)
	}
}
