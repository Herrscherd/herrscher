package host

import (
	"context"
	"path/filepath"
	"testing"
)

func TestHookOffersOnlyAnAllowedCommandToRTK(t *testing.T) {
	calls := 0
	counting := func(_ context.Context, command string) (string, bool) {
		calls++
		return "rtk " + command, true
	}
	deny := func(_ context.Context, _ []string) (string, bool, error) {
		return `{"decision":"deny","reason":"no"}`, true, nil
	}
	runRewritingHook(t, rtkBashPayload("git status"), counting, deny)
	if calls != 0 {
		t.Fatalf("a denied command was offered to rtk %d times", calls)
	}
	noDaemon := func(_ context.Context, _ []string) (string, bool, error) {
		return "", false, nil
	}
	runRewritingHook(t, rtkBashPayload("git status"), counting, noDaemon)
	if calls != 0 {
		t.Fatalf("a refused command was offered to rtk %d times", calls)
	}
}

func TestMeasuredRTKReadsTheProjectHookTheRewriterHonours(t *testing.T) {
	fakeRTKOnPath(t)
	t.Setenv("HOME", t.TempDir())
	project := t.TempDir()
	t.Chdir(project)
	writeFile(t, filepath.Join(project, ".claude", "settings.local.json"), `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"command":"rtk hook claude"}]}]}}`)
	if !measuredRTK().UserHook {
		t.Fatal("the health probe missed the project hook that the rewriter honours")
	}
	if got, ok := rtkRewriter(true)(context.Background(), "git status"); ok {
		t.Fatalf("rewrote to %q although the project hook already does", got)
	}
}
