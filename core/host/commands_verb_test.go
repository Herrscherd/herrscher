package host

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/supervisor"
)

func TestCommandsVerbDescribesTheRegistry(t *testing.T) {
	approvalTab := newApprovalTable()
	sessionTab := newSessionTable()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st := state.NewState(t.TempDir() + "/s.json")
	sup := supervisor.NewSupervisor(ctx, "/nonexistent/herrscher")
	reg, _, err := buildRegistry(ctx, Deps{}, Options{StatePath: t.TempDir() + "/s.json", DefaultCmd: "claude"}, st, sup, "", nil, approvalTab, sessionTab)
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
	have := make(map[string]bool)
	for _, s := range specs {
		have[strings.Join(s.Path, " ")] = true
	}
	for _, want := range []string{"commands", "session create", "session list"} {
		if !have[want] {
			t.Errorf("commands --json omits %q", want)
		}
	}

	if text, err := reg.Dispatch(ctx, []string{"commands"}); err != nil || !strings.Contains(text, "session create") {
		t.Fatalf("bare commands = %q, %v", text, err)
	}
}

func TestSkillVerbsAreRegistered(t *testing.T) {
	approvalTab := newApprovalTable()
	sessionTab := newSessionTable()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st := state.NewState(t.TempDir() + "/s.json")
	sup := supervisor.NewSupervisor(ctx, "/nonexistent/herrscher")
	reg, _, err := buildRegistry(ctx, Deps{}, Options{StatePath: t.TempDir() + "/s.json", DefaultCmd: "claude"}, st, sup, "", nil, approvalTab, sessionTab)
	if err != nil {
		t.Fatal(err)
	}

	have := make(map[string]bool)
	for _, s := range reg.Specs() {
		have[strings.Join(s.Path, " ")] = true
	}
	for _, want := range []string{"skill list", "skill approve"} {
		if !have[want] {
			t.Errorf("%q is not dispatched", want)
		}
	}
}
