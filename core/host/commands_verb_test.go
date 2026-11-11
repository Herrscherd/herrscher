package host

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/supervisor"
)

func TestCommandsVerbDescribesTheRegistry(t *testing.T) {
	approvalTab := newApprovalTable()
	sessionTab := newSessionTable()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st := state.NewState(t.TempDir() + "/s.json")
	sup := supervisor.NewSupervisor(ctx, "/nonexistent/herrscher", testRuntime(t))
	reg, _, err := buildRegistry(ctx, Deps{}, Options{Runtime: testRuntime(t), StatePath: t.TempDir() + "/s.json", DefaultCmd: "claude", Language: i18n.English}, st, sup, "", nil, approvalTab, sessionTab)
	if err != nil {
		t.Fatal(err)
	}

	out, err := cli.Plain(reg.Dispatch(ctx, []string{"commands", "--json"}))
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

	if text, err := cli.Plain(reg.Dispatch(ctx, []string{"commands"})); err != nil || !strings.Contains(text, "session create") {
		t.Fatalf("bare commands = %q, %v", text, err)
	}
}

func TestSkillVerbsAreRegistered(t *testing.T) {
	approvalTab := newApprovalTable()
	sessionTab := newSessionTable()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st := state.NewState(t.TempDir() + "/s.json")
	sup := supervisor.NewSupervisor(ctx, "/nonexistent/herrscher", testRuntime(t))
	reg, _, err := buildRegistry(ctx, Deps{}, Options{Runtime: testRuntime(t), StatePath: t.TempDir() + "/s.json", DefaultCmd: "claude", Language: i18n.English}, st, sup, "", nil, approvalTab, sessionTab)
	if err != nil {
		t.Fatal(err)
	}

	have := make(map[string]bool)
	specs, specsErr := reg.Specs(i18n.English)
	if specsErr != nil {
		t.Fatal(specsErr)
	}
	for _, s := range specs {
		have[strings.Join(s.Path, " ")] = true
	}
	for _, want := range []string{"skill list", "skill approve"} {
		if !have[want] {
			t.Errorf("%q is not dispatched", want)
		}
	}
}

func TestCommandsVerbListsThenWarnsOnARawHelpText(t *testing.T) {
	var reg cli.Registry
	raw := contracts.New("rawdemo", "show").Help("rawdemo.help.undeclared").
		Do(func(context.Context, contracts.Input) (contracts.Result, error) { return contracts.Result{}, nil })
	if err := reg.Add(raw); err != nil {
		t.Fatal(err)
	}
	if err := reg.Add(commandsVerb(fixedLanguage(i18n.English), &reg)); err != nil {
		t.Fatal(err)
	}
	out, err := cli.Plain(reg.Dispatch(context.Background(), []string{"commands"}))
	if err != nil {
		t.Fatalf("commands must not fail on a raw help text: %v", err)
	}
	if !strings.Contains(out, "  rawdemo show") {
		t.Fatalf("commands output misses the listed command:\n%s", out)
	}
	if !strings.Contains(out, `command "rawdemo show"`) || !strings.Contains(out, "rawdemo.help.undeclared") {
		t.Fatalf("commands output does not warn with the command and the key:\n%s", out)
	}
}

func TestCommandsJSONWarnsThroughTheLogOnARawHelpText(t *testing.T) {
	var reg cli.Registry
	raw := contracts.New("rawdemo", "show").Help("rawdemo.help.undeclared").
		Do(func(context.Context, contracts.Input) (contracts.Result, error) { return contracts.Result{}, nil })
	if err := reg.Add(raw); err != nil {
		t.Fatal(err)
	}
	if err := reg.Add(commandsVerb(fixedLanguage(i18n.English), &reg)); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	out, err := cli.Plain(reg.Dispatch(context.Background(), []string{"commands", "--json"}))
	if err != nil {
		t.Fatalf("commands --json must not fail on a raw help text: %v", err)
	}
	if !json.Valid([]byte(out)) {
		t.Fatalf("commands --json output is not JSON:\n%s", out)
	}
	logged := logs.String()
	if !strings.Contains(logged, "command help texts not fully rendered") {
		t.Fatalf("no warning logged for the raw help text:\n%s", logged)
	}
	if !strings.Contains(logged, "rawdemo show") || !strings.Contains(logged, "rawdemo.help.undeclared") {
		t.Fatalf("the logged warning does not name the command and the key:\n%s", logged)
	}
}
