package checkpoint

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

const refFieldCount = 2

// RefMove is one ref a rekey renames, with the commit it points to.
type RefMove struct {
	From       string
	To         string
	Commit     string
	replaced   string
	undo       bool
	landed     bool
	renumbered bool
}

// Rekey moves the checkpoints and the undo point filed under one key to
// another, in the repository of Dir. Plan it with [PlanRekey] and run it with
// [Store.ApplyRekey].
type Rekey struct {
	Dir    string
	Moves  []RefMove
	target string
}

// RefsOf returns the ref prefix the checkpoints filed under key live in.
func RefsOf(key string) string {
	return checkpointPrefix(key)
}

// UndoRefOf returns the ref the undo point filed under key lives at.
func UndoRefOf(key string) string {
	return undoRef(key)
}

// Empty reports whether the rekey has nothing to move.
func (r Rekey) Empty() bool {
	return len(r.Moves) == 0
}

// RekeyTally counts what a rekey does: the checkpoints it renames under their
// own index, those it files after the last index the new key already holds,
// whether it renames the undo point and whether that replaces the undo point
// of the new key, and the refs already at their new name whose old name it
// only deletes.
type RekeyTally struct {
	Checkpoints  int
	Renumbered   int
	FirstIndex   int
	Undo         bool
	UndoReplaced bool
	Finished     int
}

// Tally counts the refs of the rekey by what applying it does to them.
func (r Rekey) Tally() RekeyTally {
	var tally RekeyTally
	for _, move := range r.Moves {
		switch {
		case move.landed:
			tally.Finished++
		case move.undo:
			tally.Undo, tally.UndoReplaced = true, move.replaced != ""
		case move.renumbered:
			tally.Renumbered++
			if index, ok := refIndex(move.To, r.target); ok && (tally.FirstIndex == 0 || index < tally.FirstIndex) {
				tally.FirstIndex = index
			}
		default:
			tally.Checkpoints++
		}
	}
	return tally
}

// PlanRekey lists the refs filed under from in the repository of dir and the
// names they take under target. A ref already at its new name with the same
// commit counts as moved, so a plan made after an interrupted rekey finishes
// it. When a checkpoint index of from is already taken under target by
// another commit, as after an older build ran on a migrated state and filed
// new checkpoints under the session name again, every checkpoint still to move
// is filed after the last index target holds, in its order, so none is lost.
// An undo point of from replaces the one of target, as a later rewind would.
// It returns an empty plan when from cannot name a ref, since nothing can be
// filed under it. A ref under from that names no index and whose new name
// holds another commit is no checkpoint the store lists, and stays in place.
func PlanRekey(ctx context.Context, dir, from, target string) (Rekey, error) {
	if !usableKey(from) {
		return Rekey{Dir: dir}, nil
	}
	if err := validKey(target); err != nil {
		return Rekey{}, err
	}
	sources, err := listRefs(ctx, dir, checkpointPrefix(from), undoRef(from))
	if err != nil {
		return Rekey{}, err
	}
	targets, err := refsUnder(ctx, dir, checkpointPrefix(target), undoRef(target))
	if err != nil {
		return Rekey{}, err
	}
	plan := Rekey{Dir: dir, target: target}
	clash := false
	for _, source := range sources {
		move := RefMove{From: source.name, To: rekeyedName(source.name, from, target), Commit: source.commit, undo: source.name == undoRef(from)}
		held, taken := targets[move.To]
		_, numbered := refIndex(move.From, from)
		switch {
		case !taken:
		case held == move.Commit:
			move.landed = true
		case move.undo:
			move.replaced = held
		case numbered:
			clash = true
		default:
			continue
		}
		plan.Moves = append(plan.Moves, move)
	}
	if clash {
		renumber(plan.Moves, from, target, targets)
	}
	return plan, nil
}

func renumber(moves []RefMove, from, target string, targets map[string]string) {
	next := 1
	for name := range targets {
		if index, ok := refIndex(name, target); ok && index >= next {
			next = index + 1
		}
	}
	slices.SortStableFunc(moves, func(left, right RefMove) int {
		leftIndex, _ := refIndex(left.From, from)
		rightIndex, _ := refIndex(right.From, from)
		return cmp.Compare(leftIndex, rightIndex)
	})
	for idx := range moves {
		move := &moves[idx]
		if _, numbered := refIndex(move.From, from); !numbered || move.landed {
			continue
		}
		move.To, move.renumbered = checkpointRef(target, next), true
		next++
	}
}

func refIndex(ref, key string) (int, bool) {
	rest, ok := strings.CutPrefix(ref, checkpointPrefix(key))
	if !ok {
		return 0, false
	}
	index, err := strconv.Atoi(rest)
	return index, err == nil
}

// ApplyRekey renames every ref of plan in one git transaction: either all of
// them move, or none does.
func (s *Store) ApplyRekey(ctx context.Context, plan Rekey) error {
	if plan.Empty() {
		return nil
	}
	var script bytes.Buffer
	for _, move := range plan.Moves {
		switch {
		case move.landed:
		case move.replaced != "":
			fmt.Fprintf(&script, "update %s %s %s\n", move.To, move.Commit, move.replaced)
		default:
			fmt.Fprintf(&script, "create %s %s\n", move.To, move.Commit)
		}
		fmt.Fprintf(&script, "delete %s %s\n", move.From, move.Commit)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := git(ctx, plan.Dir, nil, &script, "update-ref", "--stdin")
	return err
}

type namedRef struct {
	name   string
	commit string
}

func refsUnder(ctx context.Context, dir string, patterns ...string) (map[string]string, error) {
	listed, err := listRefs(ctx, dir, patterns...)
	if err != nil {
		return nil, err
	}
	refs := make(map[string]string, len(listed))
	for _, ref := range listed {
		refs[ref.name] = ref.commit
	}
	return refs, nil
}

func listRefs(ctx context.Context, dir string, patterns ...string) ([]namedRef, error) {
	args := append([]string{"for-each-ref", "--format=%(refname)%00%(objectname)"}, patterns...)
	out, err := git(ctx, dir, nil, nil, args...)
	if err != nil {
		return nil, err
	}
	var refs []namedRef
	for line := range strings.Lines(string(out)) {
		fields := strings.Split(strings.TrimRight(line, "\n"), "\x00")
		if len(fields) != refFieldCount {
			continue
		}
		refs = append(refs, namedRef{name: fields[0], commit: fields[1]})
	}
	return refs, nil
}

func rekeyedName(ref, from, target string) string {
	if ref == undoRef(from) {
		return undoRef(target)
	}
	return checkpointPrefix(target) + strings.TrimPrefix(ref, checkpointPrefix(from))
}

func usableKey(key string) bool {
	return validKey(key) == nil
}
