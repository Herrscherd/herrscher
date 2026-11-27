package host

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/health"
)

func rtkBashPayload(command string) string {
	return `{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":` + mustJSONString(command) + `}}`
}

func mustJSONString(s string) string {
	out, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(out)
}

func fixedRewrite(rewritten string) commandRewriter {
	return func(_ context.Context, _ string) (string, bool) {
		if rewritten == "" {
			return "", false
		}
		return rewritten, true
	}
}

func hookUpdatedInput(t *testing.T, out string) map[string]any {
	t.Helper()
	var got struct {
		HookSpecificOutput struct {
			UpdatedInput map[string]any `json:"updatedInput"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("hook wrote unparseable json %q: %v", out, err)
	}
	return got.HookSpecificOutput.UpdatedInput
}

func runRewritingHook(t *testing.T, payload string, rewrite commandRewriter, ask hookAsker) string {
	t.Helper()
	pinEnglish(t)
	hookSession(t)
	var out, errOut strings.Builder
	if err := runPermissionHook(context.Background(), NewMessages(), strings.NewReader(payload), &out, &errOut, testTarget, hookAnswerWait(), ask, rewrite); err != nil {
		t.Fatalf("the hook could not deliver its decision: %v", err)
	}
	return out.String()
}

func allowAll(seen *[]string) hookAsker {
	return func(_ context.Context, argv []string) (string, bool, error) {
		*seen = argv
		return `{"decision":"allow"}`, true, nil
	}
}

func TestHookRewritesAnAllowedBashCommand(t *testing.T) {
	var seen []string
	out := runRewritingHook(t, rtkBashPayload("git status"), fixedRewrite("rtk git status"), allowAll(&seen))
	if d, _ := hookDecision(t, out); d != "allow" {
		t.Fatalf("decision %q, want allow", d)
	}
	if got := hookUpdatedInput(t, out)["command"]; got != "rtk git status" {
		t.Fatalf("updatedInput command %v, want rtk git status", got)
	}
}

func TestHookAppliesTheApprovalToTheCommandAsWritten(t *testing.T) {
	var seen []string
	out := runRewritingHook(t, rtkBashPayload("git status"), fixedRewrite("rtk git status"), allowAll(&seen))
	if subject := seen[len(seen)-1]; subject != "git status" {
		t.Fatalf("approval asked about %q, want the command as written", subject)
	}
	if hookUpdatedInput(t, out) == nil {
		t.Fatal("an allowed rewrite carried no updatedInput")
	}
}

func TestHookNeverRewritesADeniedCommand(t *testing.T) {
	var seen []string
	deny := func(_ context.Context, argv []string) (string, bool, error) {
		seen = argv
		return `{"decision":"deny","reason":"herrscher: no"}`, true, nil
	}
	out := runRewritingHook(t, rtkBashPayload("git push origin main"), fixedRewrite("rtk git push origin main"), deny)
	d, reason := hookDecision(t, out)
	if d != "deny" || reason != "herrscher: no" {
		t.Fatalf("got %q/%q, want deny/herrscher: no", d, reason)
	}
	if hookUpdatedInput(t, out) != nil {
		t.Fatal("a denied command was handed a rewritten input")
	}
	if subject := seen[len(seen)-1]; subject != "git push origin main" {
		t.Fatalf("approval asked about %q, want the command as written", subject)
	}
}

func TestHookKeepsTheCommandWhenRewritingIsOff(t *testing.T) {
	var seen []string
	out := runRewritingHook(t, rtkBashPayload("git status"), nil, allowAll(&seen))
	if d, _ := hookDecision(t, out); d != "allow" {
		t.Fatalf("decision %q, want allow", d)
	}
	if strings.Contains(out, "updatedInput") {
		t.Fatalf("the hook rewrote a command with rewriting off: %s", out)
	}
}

func TestHookFailsOpenWhenTheRewriteRefuses(t *testing.T) {
	var seen []string
	out := runRewritingHook(t, rtkBashPayload("git status"), fixedRewrite(""), allowAll(&seen))
	if d, _ := hookDecision(t, out); d != "allow" {
		t.Fatalf("decision %q, want allow", d)
	}
	if strings.Contains(out, "updatedInput") {
		t.Fatalf("a refused rewrite still changed the command: %s", out)
	}
}

func TestHookRewritesOnlyBashCalls(t *testing.T) {
	calls := 0
	counting := func(_ context.Context, command string) (string, bool) {
		calls++
		return "rtk " + command, true
	}
	var seen []string
	payload := `{"hook_event_name":"PreToolUse","tool_name":"Read","tool_input":{"file_path":"/tmp/a"}}`
	out := runRewritingHook(t, payload, counting, allowAll(&seen))
	if calls != 0 {
		t.Fatalf("a Read call was offered to the rewrite %d times", calls)
	}
	if strings.Contains(out, "updatedInput") {
		t.Fatalf("a Read call was rewritten: %s", out)
	}
}

func TestHookKeepsTheOtherFieldsOfTheToolInput(t *testing.T) {
	var seen []string
	payload := `{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"ls","description":"list","timeout":5000}}`
	out := runRewritingHook(t, payload, fixedRewrite("rtk ls"), allowAll(&seen))
	input := hookUpdatedInput(t, out)
	if input["command"] != "rtk ls" || input["description"] != "list" || input["timeout"] != float64(5000) {
		t.Fatalf("updatedInput %v lost a field of the original input", input)
	}
}

func TestRTKRewriterIsOffByDefault(t *testing.T) {
	if rtkRewriter(false) != nil {
		t.Fatal("the rtk rewriter exists with the switch off")
	}
}

func TestRTKRewriterStandsDownForTheUsersOwnHook(t *testing.T) {
	fakeRTKOnPath(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(t.TempDir())
	writeFile(t, filepath.Join(home, ".claude", "settings.json"), `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"rtk hook claude"}]}]}}`)
	if got, ok := rtkRewriter(true)(context.Background(), "git status"); ok {
		t.Fatalf("rewrote to %q although the user hook already does", got)
	}
}

func TestRTKRewriterRewritesWhenNoUserHookIsInstalled(t *testing.T) {
	fakeRTKOnPath(t)
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())
	got, ok := rtkRewriter(true)(context.Background(), "git status")
	if !ok || got != "rtk git status" {
		t.Fatalf("rewrite = %q, %v; want rtk git status, true", got, ok)
	}
}

func TestRTKRewriterStandsDownWhenRTKIsNotOnPath(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())
	if got, ok := rtkRewriter(true)(context.Background(), "git status"); ok {
		t.Fatalf("rewrote to %q without rtk on PATH", got)
	}
}

func TestRTKRewriterStandsDownWhenTheProjectHasTheUserHook(t *testing.T) {
	fakeRTKOnPath(t)
	t.Setenv("HOME", t.TempDir())
	project := t.TempDir()
	t.Chdir(project)
	writeFile(t, filepath.Join(project, ".claude", "settings.local.json"), `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"command":"rtk-rewrite"}]}]}}`)
	if got, ok := rtkRewriter(true)(context.Background(), "git status"); ok {
		t.Fatalf("rewrote to %q although the project hook already does", got)
	}
}

func TestParseHookOptionsReadsTheRTKFlag(t *testing.T) {
	if !ParseHookOptions([]string{"--rtk"}).RTK {
		t.Fatal("--rtk did not switch rtk on")
	}
	if ParseHookOptions(nil).RTK || ParseHookOptions([]string{"--other"}).RTK {
		t.Fatal("rtk switched on without its flag")
	}
}

func TestRTKHealthProbeIsOffWhenRTKIsAbsent(t *testing.T) {
	probes := rtkProbes(healthInputs{rtk: func() health.RTK { return health.RTK{UserHook: false} }})
	if len(probes) != 1 {
		t.Fatalf("got %d probes, want 1", len(probes))
	}
	report, _ := probes[0].Run(context.Background(), false)
	if report.State != contracts.HealthOff {
		t.Fatalf("state %v, want off", report.State)
	}
}

func TestRTKHealthProbeIsOKWhenRTKIsPresent(t *testing.T) {
	probes := rtkProbes(healthInputs{rtk: func() health.RTK { return health.RTK{Present: true, Version: "1.0.1"} }})
	report, _ := probes[0].Run(context.Background(), false)
	if report.State != contracts.HealthOK {
		t.Fatalf("state %v, want ok", report.State)
	}
}

func TestRTKHealthProbeIsAbsentWithoutMeasurement(t *testing.T) {
	if probes := rtkProbes(healthInputs{}); probes != nil {
		t.Fatalf("got %d probes without a measurement", len(probes))
	}
}

func fakeRTKOnPath(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "rtk"), "#!/bin/sh\nprintf 'rtk %s' \"$2\"\n")
	if err := os.Chmod(filepath.Join(dir, "rtk"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
