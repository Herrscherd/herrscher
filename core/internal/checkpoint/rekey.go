package checkpoint

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
)

const refFieldCount = 2

// ErrRekeyConflict is the cause of a rekey refused because a ref it would write
// already holds another commit.
var ErrRekeyConflict = errors.New("checkpoint ref already holds another commit")

// RekeyConflictError names the ref a rekey would write, the commit it already
// holds, and the commit the rekey would have put there.
type RekeyConflictError struct {
	Ref  string
	Have string
	Want string
}

func (e *RekeyConflictError) Error() string {
	return fmt.Sprintf("%s: %s holds %s, want %s", ErrRekeyConflict, e.Ref, e.Have, e.Want)
}

func (e *RekeyConflictError) Unwrap() error { return ErrRekeyConflict }

// RefMove is one ref a rekey renames, with the commit it points to.
type RefMove struct {
	From   string
	To     string
	Commit string
	landed bool
}

// Rekey moves the checkpoints and the undo point filed under one key to
// another, in the repository of Dir. Plan it with [PlanRekey] and run it with
// [Store.ApplyRekey].
type Rekey struct {
	Dir   string
	Moves []RefMove
}

// RefsOf returns the ref prefix the checkpoints filed under key live in.
func RefsOf(key string) string {
	return checkpointPrefix(key)
}

// Empty reports whether the rekey has nothing to move.
func (r Rekey) Empty() bool {
	return len(r.Moves) == 0
}

// PlanRekey lists the refs filed under from in the repository of dir and the
// names they take under target. A ref already at its new name with the same commit
// counts as moved, so a plan made after an interrupted rekey finishes it. It
// returns a [*RekeyConflictError] when a new name already holds another commit,
// and an empty plan when from cannot name a ref, since nothing can be filed
// under it.
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
	plan := Rekey{Dir: dir}
	for _, source := range sources {
		move := RefMove{From: source.name, To: rekeyedName(source.name, from, target), Commit: source.commit}
		if held, ok := targets[move.To]; ok {
			if held != move.Commit {
				return Rekey{}, &RekeyConflictError{Ref: move.To, Have: held, Want: move.Commit}
			}
			move.landed = true
		}
		plan.Moves = append(plan.Moves, move)
	}
	return plan, nil
}

// ApplyRekey renames every ref of plan in one git transaction: either all of
// them move, or none does.
func (s *Store) ApplyRekey(ctx context.Context, plan Rekey) error {
	if plan.Empty() {
		return nil
	}
	var script bytes.Buffer
	for _, move := range plan.Moves {
		if !move.landed {
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
