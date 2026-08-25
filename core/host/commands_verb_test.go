package host

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/supervisor"
)

// The TUI's command palette is derived from `commands --json`, so this verb is
// the contract that keeps the menu and the dispatcher from drifting apart. It
// must exist, answer JSON, and describe the registry it lives in — including
// itself, since it is registered last precisely so it sees a complete one.
func TestCommandsVerbDescribesTheRegistry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st := state.NewState(t.TempDir() + "/s.json")
	sup := supervisor.NewSupervisor(ctx, "/nonexistent/herrscher")
	reg, _, err := buildRegistry(ctx, Deps{}, Options{StatePath: t.TempDir() + "/s.json", DefaultCmd: "claude"}, st, sup, "", nil)
	if err != nil {
		t.Fatal(err)
	}

	out, err := reg.Dispatch(ctx, []string{"commands", "--json"})
	if err != nil {
		t.Fatalf("commands --json: %v", err)
	}
	var specs []struct {
		Path []string `json:"path"`
		Help string   `json:"help"`
	}
	if err := json.Unmarshal([]byte(out), &specs); err != nil {
		t.Fatalf("commands --json did not answer JSON: %v (%q)", err, out)
	}
	have := map[string]bool{}
	for _, s := range specs {
		have[strings.Join(s.Path, " ")] = true
	}
	for _, want := range []string{"commands", "session create", "session list"} {
		if !have[want] {
			t.Errorf("commands --json omits %q", want)
		}
	}

	// Without --json it is the human help, which is what a bare `commands` at a
	// shell should print.
	if text, err := reg.Dispatch(ctx, []string{"commands"}); err != nil || !strings.Contains(text, "session create") {
		t.Fatalf("bare commands = %q, %v", text, err)
	}
}

// The two verbs the self-authored-skill loop asks of an operator: seeing what an
// agent wrote for itself, and deciding whether the project's other agents may run
// it. What is held here is registration, because a handler that exists in the
// source but not in the registry is a handler the daemon answers "unknown
// command" to, and the approval it gates is the one step the loop cannot take on
// its own.
func TestSkillVerbsAreRegistered(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st := state.NewState(t.TempDir() + "/s.json")
	sup := supervisor.NewSupervisor(ctx, "/nonexistent/herrscher")
	reg, _, err := buildRegistry(ctx, Deps{}, Options{StatePath: t.TempDir() + "/s.json", DefaultCmd: "claude"}, st, sup, "", nil)
	if err != nil {
		t.Fatal(err)
	}

	have := map[string]bool{}
	for _, s := range reg.Specs() {
		have[strings.Join(s.Path, " ")] = true
	}
	for _, want := range []string{"skill list", "skill approve"} {
		if !have[want] {
			t.Errorf("%q is not dispatched", want)
		}
	}
}
