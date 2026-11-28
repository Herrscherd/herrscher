package checkpoint

import (
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

func rekeyAlpha(t *testing.T, store *Store, dir string) Rekey {
	t.Helper()
	plan, err := PlanRekey(t.Context(), dir, "alpha", rekeyTarget)
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

	if plan := rekeyAlpha(t, &store, dir); len(plan.Moves) != 3 {
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
	if again := rekeyAlpha(t, &store, dir); !again.Empty() {
		t.Fatalf("a second rekey plans %+v, want nothing", again.Moves)
	}
}

func TestRekeyFinishesARefAlreadyAtItsNewName(t *testing.T) {
	var store Store
	dir := initRepo(t)
	before := takeTwoAndRewind(t, &store, dir, "alpha")
	run(t, dir, "update-ref", checkpointRef(rekeyTarget, 1), before[0].Commit)

	rekeyAlpha(t, &store, dir)
	if left := checkpointsUnder(t, &store, dir, "alpha"); len(left) != 0 {
		t.Fatalf("the old key keeps %+v", left)
	}
	if after := checkpointsUnder(t, &store, dir, rekeyTarget); len(after) != 2 {
		t.Fatalf("checkpoints under the new key = %+v, want both", after)
	}
}

func TestRekeyFilesCheckpointsTakenAgainUnderTheNameAfterTheLastIndexOfTheNewKey(t *testing.T) {
	var store Store
	dir := initRepo(t)
	migrated := takeTwoAndRewind(t, &store, dir, "alpha")
	rekeyAlpha(t, &store, dir)
	write(t, dir, "older-build.txt", "taken by an older build\n")
	again := takeTwoAndRewind(t, &store, dir, "alpha")
	laterUndo := refAt(t, dir, undoRef("alpha"))

	plan := rekeyAlpha(t, &store, dir)
	if tally := plan.Tally(); tally.Renumbered != 2 || tally.FirstIndex != 3 || !tally.UndoReplaced {
		t.Fatalf("tally = %+v, want two checkpoints filed from index 3 and the undo point replaced", tally)
	}
	if left := checkpointsUnder(t, &store, dir, "alpha"); len(left) != 0 {
		t.Fatalf("the old key keeps %+v", left)
	}
	after := checkpointsUnder(t, &store, dir, rekeyTarget)
	want := []string{migrated[0].Commit, migrated[1].Commit, again[0].Commit, again[1].Commit}
	if len(after) != len(want) {
		t.Fatalf("checkpoints under the new key = %+v, want %d", after, len(want))
	}
	for idx, entry := range after {
		if entry.Index != idx+1 || entry.Commit != want[idx] {
			t.Fatalf("checkpoint %d = %+v, want index %d at %s", idx, entry, idx+1, want[idx])
		}
	}
	if undo := refAt(t, dir, undoRef(rekeyTarget)); laterUndo == "" || undo != laterUndo {
		t.Fatalf("undo point of the new key = %q, want %q, the one of the later rewind", undo, laterUndo)
	}
}

func refAt(t *testing.T, dir, ref string) string {
	t.Helper()
	commit, err := resolveRef(t.Context(), dir, ref)
	if err != nil {
		t.Fatal(err)
	}
	return commit
}

func TestRekeyLeavesAKeyThatOnlySharesThePrefixAlone(t *testing.T) {
	var store Store
	dir := initRepo(t)
	takeTwoAndRewind(t, &store, dir, "alpha")
	other := takeTwoAndRewind(t, &store, dir, "alpha2")

	for _, move := range rekeyAlpha(t, &store, dir).Moves {
		if strings.Contains(move.From, "alpha2") {
			t.Fatalf("rekey of alpha moved %s", move.From)
		}
	}
	if kept := checkpointsUnder(t, &store, dir, "alpha2"); len(kept) != len(other) || !store.HasUndo(t.Context(), dir, "alpha2") {
		t.Fatalf("alpha2 keeps %+v, want its own checkpoints and undo point", kept)
	}
}
