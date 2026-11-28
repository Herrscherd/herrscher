package worktree

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/shellquote"
)

func localAlphaRepo(t *testing.T) (string, string) {
	t.Helper()
	repo := initRepo(t)
	return repo, bareAlphaWorktree(t, repo)
}

func authRequiredURL(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)
	return server.URL + "/private.git"
}

func silentURL(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(func() {
		server.CloseClientConnections()
		server.Close()
	})
	return server.URL + "/hung.git"
}

func unreachableParams(t *testing.T, err error) clitext.Params {
	t.Helper()
	failure, ok := errors.AsType[FailureError](err)
	if !ok || failure.Key != clitext.WorktreeRelocateRemoteUnreachable {
		t.Fatalf("err = %v, want a %s refusal", err, clitext.WorktreeRelocateRemoteUnreachable)
	}
	return failure.Params
}

func recordingAskpass(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	marker := filepath.Join(dir, "asked")
	script := filepath.Join(dir, "askpass")
	body := "#!/bin/sh\ntouch " + shellquote.Quote(marker) + "\necho secret\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_ASKPASS", script)
	t.Setenv("SSH_ASKPASS", script)
	t.Setenv("SSH_ASKPASS_REQUIRE", "force")
	t.Setenv("DISPLAY", ":0")
	return marker
}

func TestPlanBareRelocationRefusesWithoutPromptingWhenOriginWantsCredentials(t *testing.T) {
	repo, from := localAlphaRepo(t)
	gitIn(t, repo, "remote", "add", "origin", authRequiredURL(t))
	marker := recordingAskpass(t)
	w := NewWorktreer(clitext.NewMessages(), "inst")
	started := time.Now()
	_, err := w.PlanBareRelocation(t.Context(), BareSession{Name: "alpha", Path: from, PullRequest: 12})
	unreachableParams(t, err)
	if elapsed := time.Since(started); elapsed > 20*time.Second {
		t.Fatalf("asking origin took %s, want an immediate refusal", elapsed)
	}
	if _, statErr := os.Stat(marker); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("asking origin ran the askpass program (stat %v), want no prompt at all", statErr)
	}
	text := err.Error()
	for _, want := range []string{`"alpha"`, "worktree move", "branch -m 'session/alpha' 'session/inst/alpha'"} {
		if !strings.Contains(text, want) {
			t.Errorf("refusal %q does not carry %q", text, want)
		}
	}
}

func TestPlanBareRelocationBoundsTheQuestionToOrigin(t *testing.T) {
	repo, from := localAlphaRepo(t)
	gitIn(t, repo, "remote", "add", "origin", silentURL(t))
	w := NewWorktreer(clitext.NewMessages(), "inst")
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	started := time.Now()
	_, err := w.PlanBareRelocation(ctx, BareSession{Name: "alpha", Path: from, PullRequest: 12})
	unreachableParams(t, err)
	if elapsed := time.Since(started); elapsed > 10*time.Second {
		t.Fatalf("a silent origin held the plan for %s, want it cut at its limit", elapsed)
	}
}

func TestPlanBareRelocationAsksOnlyOrigin(t *testing.T) {
	repo, from := localAlphaRepo(t)
	origin := filepath.Join(t.TempDir(), "origin.git")
	gitIn(t, repo, "init", "-q", "--bare", origin)
	gitIn(t, repo, "remote", "add", "origin", origin)
	gitIn(t, repo, "remote", "add", "upstream", "https://127.0.0.1:1/dead.git")
	plan, err := NewWorktreer(clitext.NewMessages(), "inst").PlanBareRelocation(t.Context(), BareSession{Name: "alpha", Path: from, PullRequest: 12})
	if err != nil {
		t.Fatalf("a dead second remote blocked the plan: %v", err)
	}
	if !plan.RenamesBranch() {
		t.Fatal("origin does not hold the branch, yet its rename is not planned")
	}
}

func TestPlanBareRelocationSeesABranchOnARemoteWhoseNameHasASlash(t *testing.T) {
	repo, from := localAlphaRepo(t)
	remote := filepath.Join(t.TempDir(), "team.git")
	gitIn(t, repo, "init", "-q", "--bare", remote)
	gitIn(t, repo, "remote", "add", "team/origin", remote)
	gitIn(t, repo, "push", "-q", "team/origin", "session/alpha")
	_, err := NewWorktreer(clitext.NewMessages(), "inst").PlanBareRelocation(t.Context(), BareSession{Name: "alpha", Path: from})
	failure, ok := errors.AsType[FailureError](err)
	if !ok || failure.Key != clitext.WorktreeRelocateRemoteBranch {
		t.Fatalf("err = %v, want a %s refusal", err, clitext.WorktreeRelocateRemoteBranch)
	}
	if got := failure.Params[clitext.ParamTarget]; got != "team/origin" {
		t.Fatalf("refusal names remote %v, want team/origin", got)
	}
}

func TestTheOfflineCommandsOfAnUnreachableRefusalLetTheMigrationThrough(t *testing.T) {
	repo, from := localAlphaRepo(t)
	gitIn(t, repo, "remote", "add", "origin", "https://127.0.0.1:1/dead.git")
	w := NewWorktreer(clitext.NewMessages(), "inst")
	sess := BareSession{Name: "alpha", Path: from, PullRequest: 12}
	_, err := w.PlanBareRelocation(t.Context(), sess)
	command, ok := unreachableParams(t, err)[clitext.ParamCommand].(string)
	if !ok {
		t.Fatal("the unreachable refusal carries no command to run offline")
	}
	if out, runErr := exec.CommandContext(t.Context(), "sh", "-c", command).CombinedOutput(); runErr != nil {
		t.Fatalf("offline commands %q failed: %v\n%s", command, runErr, out)
	}
	plan, err := w.PlanBareRelocation(t.Context(), sess)
	if err != nil {
		t.Fatalf("after the offline commands the migration still refuses: %v", err)
	}
	if plan.MovesDir() || plan.RenamesBranch() || !plan.DirAtTarget() {
		t.Fatalf("plan = %+v, want the session already in place", plan)
	}
}
