//go:build !windows

package vendortest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const (
	allowHook       = `printf '%s' '{"hookSpecificOutput":{"permissionDecision":"allow"}}'`
	bashPayloadHook = `grep -q '"tool_name":"Bash"' && printf '%s' '{"hookSpecificOutput":{"permissionDecision":"allow"}}'`
	failingHook     = `exit 3`
	bashToolLine    = `tool Bash {"command":"ls"}`
	bashToolSummary = `tool_use:Bashtoolu_1{"command":"ls"}`
)

func denyHook(reason string) string {
	return fmt.Sprintf(`printf '%%s' '{"hookSpecificOutput":{"permissionDecision":"deny","permissionDecisionReason":%q}}'`, reason)
}

func chdirWithHooks(t *testing.T, commands ...string) {
	t.Helper()
	var group hookGroup
	for _, command := range commands {
		group.Hooks = append(group.Hooks, hookCommand{Command: command})
	}
	var doc settingsDoc
	doc.Hooks.PreToolUse = []hookGroup{group}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".claude", "settings.local.json"), data, privateFile); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
}

func playWithHooks(t *testing.T, script string, hooks ...string) []string {
	t.Helper()
	chdirWithHooks(t, hooks...)
	code, events := run(t, scriptFiles(t, script), promptLine(t, "go"))
	if code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	return summary(events)
}

func TestHookVerdictReachesTheScript(t *testing.T) {
	for _, tc := range []struct {
		name   string
		script string
		hooks  []string
		want   []string
	}{
		{
			name:   "allowed by the hook",
			script: bashToolLine + "\nexpect allowed\ntext ok",
			hooks:  []string{allowHook},
			want:   []string{bashToolSummary, "tool_result:toolu_1:false:done", "text:ok", "result:ok"},
		},
		{
			name:   "denied by the hook with its reason",
			script: bashToolLine + "\nexpect denied\ntext refused",
			hooks:  []string{denyHook("not today")},
			want:   []string{bashToolSummary, "tool_result:toolu_1:true:not today", "text:refused", "result:refused"},
		},
		{
			name:   "expected allowed but denied",
			script: bashToolLine + "\nexpect allowed",
			hooks:  []string{denyHook("not today")},
			want:   []string{bashToolSummary, "tool_result:toolu_1:true:not today", "failed:expected the tool call to be allowed, the hook denied it: not today"},
		},
		{
			name:   "expected denied but allowed",
			script: bashToolLine + "\nexpect denied",
			hooks:  []string{allowHook},
			want:   []string{bashToolSummary, "tool_result:toolu_1:false:done", "failed:" + errExpectedDeny.Error()},
		},
		{
			name:   "expect with no tool before it",
			script: "expect allowed",
			want:   []string{"failed:" + errExpectNoTool.Error()},
		},
		{
			name:   "every hook must allow",
			script: bashToolLine + "\nexpect denied",
			hooks:  []string{allowHook, denyHook("second")},
			want:   []string{bashToolSummary, "tool_result:toolu_1:true:second", "result:"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := playWithHooks(t, tc.script, tc.hooks...)
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestHookReceivesTheToolCallOnStdin(t *testing.T) {
	got := playWithHooks(t, bashToolLine+"\nexpect allowed\ntext ok", bashPayloadHook)
	if !slices.Contains(got, "tool_result:toolu_1:false:done") {
		t.Fatalf("the hook did not see the Bash tool call: %q", got)
	}
}

func TestHookFailureStopsTheFake(t *testing.T) {
	chdirWithHooks(t, failingHook)
	var stderr bytes.Buffer
	code := Claude(streamArgs(), Streams{In: strings.NewReader(promptLine(t, "go")), Out: &bytes.Buffer{}, Err: &stderr}, scriptFiles(t, bashToolLine+"\nexpect allowed"))
	if code == 0 || !strings.Contains(stderr.String(), errHookFailed.Error()) {
		t.Fatalf("exit %d and stderr %q, want a failure naming the hook", code, stderr.String())
	}
}
