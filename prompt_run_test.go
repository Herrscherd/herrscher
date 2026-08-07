package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// recordingDispatcher stands in for the operator registry: it records every argv
// and answers from a scripted table, so runPromptWith can be exercised with no
// daemon, no gateway and no backend.
type recordingDispatcher struct {
	seen  [][]string
	reply map[string]string // first two argv segments -> reply
	fail  map[string]error
}

func (r *recordingDispatcher) Dispatch(_ context.Context, argv []string) (string, error) {
	r.seen = append(r.seen, append([]string(nil), argv...))
	key := strings.Join(argv[:2], " ")
	if err := r.fail[key]; err != nil {
		return "", err
	}
	return r.reply[key], nil
}

func argOf(argv []string, flag string) (string, bool) {
	for i, a := range argv {
		if a == flag && i+1 < len(argv) {
			return argv[i+1], true
		}
	}
	return "", false
}

func TestRunPromptCreatesThenSeeds(t *testing.T) {
	d := &recordingDispatcher{reply: map[string]string{"session seed": "voilà le plan"}}
	var out, errOut strings.Builder

	if err := runPromptWith(context.Background(), d, "lis-le-thread-a3fq", "lis le thread X", &out, &errOut); err != nil {
		t.Fatalf("runPromptWith: %v", err)
	}
	if len(d.seen) != 2 {
		t.Fatalf("dispatched %d commands, want create then seed: %v", len(d.seen), d.seen)
	}
	if got := strings.Join(d.seen[0][:2], " "); got != "session create" {
		t.Fatalf("first command = %q, want session create", got)
	}
	if got := strings.Join(d.seen[1][:2], " "); got != "session seed" {
		t.Fatalf("second command = %q, want session seed", got)
	}
	createName, _ := argOf(d.seen[0], "--name")
	seedName, _ := argOf(d.seen[1], "--name")
	if createName != "lis-le-thread-a3fq" || seedName != createName {
		t.Fatalf("create named %q but seed named %q", createName, seedName)
	}
	if task, _ := argOf(d.seen[1], "--task"); task != "lis le thread X" {
		t.Fatalf("seed task = %q, want the prompt verbatim", task)
	}
	if _, ok := argOf(d.seen[1], "--timeout"); !ok {
		t.Fatalf("seed must carry a timeout: %v", d.seen[1])
	}
}

// A terminal-only session is the point: the prompt path must not mint a gateway
// channel, and must not need a home to run.
func TestRunPromptCreatesATerminalOnlySession(t *testing.T) {
	d := &recordingDispatcher{reply: map[string]string{"session seed": "ok"}}
	var out, errOut strings.Builder
	if err := runPromptWith(context.Background(), d, "n-a3fq", "x y", &out, &errOut); err != nil {
		t.Fatalf("runPromptWith: %v", err)
	}
	if !slicesContain(d.seen[0], "--terminal_only") {
		t.Fatalf("create argv = %v, want --terminal_only", d.seen[0])
	}
}

func slicesContain(s []string, want string) bool {
	for _, v := range s {
		if v == want {
			return true
		}
	}
	return false
}

// The reply is the output; the session name is a note to the operator. Splitting
// them across stdout/stderr is what makes `herrscher "…" > out.md` hold the
// answer alone.
func TestRunPromptSplitsReplyFromTheSessionName(t *testing.T) {
	d := &recordingDispatcher{reply: map[string]string{"session seed": "voilà le plan"}}
	var out, errOut strings.Builder
	if err := runPromptWith(context.Background(), d, "n-a3fq", "x y", &out, &errOut); err != nil {
		t.Fatalf("runPromptWith: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "voilà le plan" {
		t.Fatalf("stdout = %q, want the reply alone", got)
	}
	if !strings.Contains(errOut.String(), "n-a3fq") {
		t.Fatalf("stderr = %q, want the session name", errOut.String())
	}
}

// A session that could not be created has nothing to seed; seeding anyway would
// hit a second, more confusing error about a session that does not exist.
func TestRunPromptDoesNotSeedWhenCreateFails(t *testing.T) {
	d := &recordingDispatcher{fail: map[string]error{"session create": fmt.Errorf("worktree: boom")}}
	var out, errOut strings.Builder
	err := runPromptWith(context.Background(), d, "n-a3fq", "x y", &out, &errOut)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v, want the create failure", err)
	}
	if len(d.seen) != 1 {
		t.Fatalf("dispatched %v, want create alone", d.seen)
	}
}

// A seed that fails leaves the session standing: it carries a worktree and the
// start of a transcript, and tearing it down is what the operator needs to
// diagnose the failure. The error names it so they can.
func TestRunPromptNamesTheSessionWhenSeedFails(t *testing.T) {
	d := &recordingDispatcher{fail: map[string]error{"session seed": fmt.Errorf("seed timeout")}}
	var out, errOut strings.Builder
	err := runPromptWith(context.Background(), d, "n-a3fq", "x y", &out, &errOut)
	if err == nil || !strings.Contains(err.Error(), "n-a3fq") {
		t.Fatalf("err = %v, want it to name the session left behind", err)
	}
}
