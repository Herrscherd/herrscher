package host

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

const prTestSecret = "ghp_hardeningsecret0123456789"

type fixedPRCredentials struct {
	token contracts.ForgeToken
}

func (c fixedPRCredentials) Token(context.Context, contracts.ForgeHost) (contracts.ForgeToken, error) {
	return c.token, nil
}

func (c fixedPRCredentials) Invalidate(context.Context, contracts.ForgeHost) {}

type blockingOpenForge struct {
	*fakePRForge

	calls   atomic.Int32
	entered chan struct{}
	release chan struct{}
}

func (f *blockingOpenForge) OpenPullRequest(ctx context.Context, draft contracts.PullRequestDraft) (contracts.PullRequest, error) {
	if f.calls.Add(1) == 1 {
		f.entered <- struct{}{}
		<-f.release
	}
	return f.fakePRForge.OpenPullRequest(ctx, draft)
}

func prStateWith(t *testing.T, sess state.Session) *state.State {
	t.Helper()
	st, err := state.LoadState(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddSession(sess); err != nil {
		t.Fatal(err)
	}
	return st
}

func prRegistryWithCredentials(t *testing.T, st *state.State, opener forgeOpener, credentials contracts.ForgeCredentials) *cli.Registry {
	t.Helper()
	reg := cli.NewRegistry(clitext.NewMessages())
	if err := addSessionPRVerbs(reg, clitext.NewMessages(), fixedLanguage(i18n.English), st, opener, credentials, nil); err != nil {
		t.Fatal(err)
	}
	return reg
}

func TestPRVerbsRefuseARemoteSession(t *testing.T) {
	dir := prFixtureRepo(t)
	st := prStateWith(t, state.Session{Name: "alpha", ChannelID: "chan-alpha", Dir: dir, Host: "vps"})
	if err := st.RecordPullRequest("alpha", state.PullRequestRecord{Number: prTestNumber, State: contracts.PullRequestOpen}); err != nil {
		t.Fatal(err)
	}
	forge := &fakePRForge{pullRequest: prNumbered(t)}
	reg := prRegistry(t, st, prForgeOpener(forge))

	for _, args := range [][]string{
		{verbPROpen, "--name", "alpha", "--title", "Titre"},
		{verbPRStatus, "--name", "alpha"},
		{verbPRChecks, "--name", "alpha"},
		{verbPRComments, "--name", "alpha"},
	} {
		_, err := runPR(t, reg, args...)
		if !errors.Is(err, errPRRemote) {
			t.Fatalf("%s on a remote session = %v, want errPRRemote", args[0], err)
		}
	}
	if len(forge.opened) != 0 {
		t.Fatalf("a remote session opened %d pull requests, want none", len(forge.opened))
	}
}

func TestPROpenReportsAPullRequestThatCouldNotBeRecorded(t *testing.T) {
	dir := prFixtureRepo(t)
	stateDir := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	st, err := state.LoadState(filepath.Join(stateDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = st.AddSession(state.Session{Name: "alpha", ChannelID: "chan-alpha", Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if err = os.RemoveAll(stateDir); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(stateDir, []byte("blocker"), 0o600); err != nil {
		t.Fatal(err)
	}
	forge := &fakePRForge{pullRequest: prNumbered(t)}
	reg := prRegistry(t, st, prForgeOpener(forge))

	_, err = runPR(t, reg, verbPROpen, "--name", "alpha", "--title", "Titre")
	if err == nil {
		t.Fatal("open reported success although the pull request was not recorded")
	}
	if len(forge.opened) != 1 {
		t.Fatalf("forge opened %d pull requests, want 1", len(forge.opened))
	}
	for _, want := range []string{"#42", "https://github.com/acme/widget/pull/42", "alpha"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not name %q: the pull request exists and the user must find it", err, want)
		}
	}
	if got, _ := st.FindSession("alpha"); got.PullRequest != 0 {
		t.Fatalf("session holds pull request %d after a failed record, want none", got.PullRequest)
	}
}

func TestPROpenReleasesItsReservationWhenOpeningFails(t *testing.T) {
	dir := prFixtureRepo(t)
	st := prState(t, dir, "")
	forge := &fakePRForge{openErr: contracts.ErrForgeUnauthorized}
	reg := prRegistry(t, st, prForgeOpener(forge))

	_, err := runPR(t, reg, verbPROpen, "--name", "alpha", "--title", "Titre")
	if !errors.Is(err, contracts.ErrForgeUnauthorized) {
		t.Fatalf("first open = %v, want the forge refusal", err)
	}
	forge.openErr = nil
	forge.pullRequest = prNumbered(t)
	if _, err := runPR(t, reg, verbPROpen, "--name", "alpha", "--title", "Titre"); err != nil {
		t.Fatalf("second open after a failed one = %v, want the reservation released", err)
	}
}

func TestPROpenRefusesASecondOpenWhileTheFirstIsInFlight(t *testing.T) {
	dir := prFixtureRepo(t)
	st := prState(t, dir, "")
	forge := &blockingOpenForge{
		fakePRForge: &fakePRForge{pullRequest: prNumbered(t)},
		entered:     make(chan struct{}, 1),
		release:     make(chan struct{}),
	}
	reg := prRegistry(t, st, prForgeOpener(forge))

	done := make(chan error, 1)
	go func() {
		_, err := runPR(t, reg, verbPROpen, "--name", "alpha", "--title", "Titre")
		done <- err
	}()
	<-forge.entered

	_, err := runPR(t, reg, verbPROpen, "--name", "alpha", "--title", "Titre")
	if !errors.Is(err, state.ErrPullRequestOpening) {
		t.Fatalf("second open while the first is in flight = %v, want state.ErrPullRequestOpening", err)
	}
	close(forge.release)
	if err := <-done; err != nil {
		t.Fatalf("first open = %v", err)
	}
	if calls := forge.calls.Load(); calls != 1 {
		t.Fatalf("forge was asked to open %d times, want 1", calls)
	}
}

func TestPROpenWithoutAnOriginHeadNamesTheBaseFlag(t *testing.T) {
	dir := prFixtureRepo(t)
	gitIn(t.Context(), t, dir, "symbolic-ref", "--delete", "refs/remotes/origin/HEAD")
	st := prState(t, dir, "")
	reg := prRegistry(t, st, prForgeOpener(&fakePRForge{pullRequest: prNumbered(t)}))

	_, err := runPR(t, reg, verbPROpen, "--name", "alpha", "--title", "Titre")
	if !errors.Is(err, errPRNoBase) {
		t.Fatalf("open without origin/HEAD = %v, want errPRNoBase", err)
	}
	if !strings.Contains(err.Error(), "--base") {
		t.Fatalf("error %q does not tell the operator to pass --base", err)
	}
}

func TestPRVerbsKeepTheTokenOutOfOutputAndErrors(t *testing.T) {
	dir := prFixtureRepo(t)
	st := prState(t, dir, "")
	if err := st.AddSession(state.Session{Name: "beta", ChannelID: "chan-beta", Dir: dir}); err != nil {
		t.Fatal(err)
	}
	token, err := contracts.NewForgeToken(prTestSecret, "bot", contracts.TokenSourceEnv)
	if err != nil {
		t.Fatal(err)
	}
	forge := &fakePRForge{pullRequest: prNumbered(t)}
	reg := prRegistryWithCredentials(t, st, prForgeOpener(forge), fixedPRCredentials{token: token})

	var seen []string
	for _, args := range [][]string{
		{verbPROpen, "--name", "alpha", "--title", "Titre", "--json"},
		{verbPRStatus, "--name", "alpha", "--json"},
		{verbPRChecks, "--name", "alpha", "--json"},
		{verbPRComments, "--name", "alpha", "--json"},
	} {
		out, runErr := runPR(t, reg, args...)
		seen = append(seen, out)
		if runErr != nil {
			seen = append(seen, runErr.Error())
		}
	}
	forge.openErr = contracts.ErrForgeUnauthorized
	_, err = runPR(t, reg, verbPROpen, "--name", "beta", "--title", "Titre")
	if !errors.Is(err, contracts.ErrForgeUnauthorized) {
		t.Fatalf("open refused by the forge = %v, want ErrForgeUnauthorized", err)
	}
	seen = append(seen, err.Error())
	forge.pullErr = contracts.ErrForgeUnauthorized
	out, err := runPR(t, reg, verbPRStatus, "--name", "alpha", "--json")
	if !errors.Is(err, contracts.ErrForgeUnauthorized) {
		t.Fatalf("status refused by the forge = %v, want ErrForgeUnauthorized", err)
	}
	seen = append(seen, out, err.Error())
	for _, text := range seen {
		if strings.Contains(text, prTestSecret) || strings.Contains(text, base64.StdEncoding.EncodeToString([]byte(prTestSecret))) {
			t.Fatalf("output or error %q carries the token", text)
		}
	}
}

func TestPRGitCarriesTheTokenInTheEnvironmentNotOnArgv(t *testing.T) {
	dir := prFixtureRepo(t)
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(t.TempDir(), "git.log")
	shim := "#!/bin/sh\n{ printf 'ARGV'; for a in \"$@\"; do printf ' %s' \"$a\"; done; printf '\\n'; env | grep '^GIT_'; } >> '" + logPath + "'\nexec '" + realGit + "' \"$@\"\n"
	shimDir := t.TempDir()
	if err = os.WriteFile(filepath.Join(shimDir, "git"), []byte(shim), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	st := prState(t, dir, "")
	token, err := contracts.NewForgeToken(prTestSecret, "bot", contracts.TokenSourceEnv)
	if err != nil {
		t.Fatal(err)
	}
	reg := prRegistryWithCredentials(t, st, prForgeOpener(&fakePRForge{pullRequest: prNumbered(t)}), fixedPRCredentials{token: token})
	if _, err = runPR(t, reg, verbPROpen, "--name", "alpha", "--title", "Titre"); err != nil {
		t.Fatalf("open: %v", err)
	}

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(raw)
	if !strings.Contains(log, "ARGV ") || !strings.Contains(log, " ls-remote ") {
		t.Fatalf("shim log %q does not show the ls-remote call", log)
	}
	if strings.Contains(log, prTestSecret) {
		t.Fatal("the raw token reached the git log")
	}
	for _, line := range strings.Split(log, "\n") {
		if strings.HasPrefix(line, "ARGV") && strings.Contains(line, base64.StdEncoding.EncodeToString([]byte(prTestSecret))) {
			t.Fatalf("argv carries the encoded token: %q", line)
		}
	}
	if !strings.Contains(log, "GIT_TERMINAL_PROMPT=0") {
		t.Fatalf("git ran without GIT_TERMINAL_PROMPT=0: %q", log)
	}
	if !strings.Contains(log, "=Authorization: Basic ") {
		t.Fatalf("git ran without the credential header in its environment: %q", log)
	}
}

func TestPRStatusJSONCarriesEveryField(t *testing.T) {
	dir := prFixtureRepo(t)
	st := prState(t, dir, "")
	if err := st.RecordPullRequest("alpha", state.PullRequestRecord{Number: prTestNumber, State: contracts.PullRequestOpen}); err != nil {
		t.Fatal(err)
	}
	reg := prRegistry(t, st, prForgeOpener(&fakePRForge{pullRequest: prNumbered(t)}))

	out, err := runPR(t, reg, verbPRStatus, "--name", "alpha", "--json")
	if err != nil {
		t.Fatalf("status --json: %v", err)
	}
	var view prStatusView
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatalf("status --json %q does not decode: %v", out, err)
	}
	want := prStatusView{
		Number: prTestNumber,
		Title:  "Titre",
		URL:    "https://github.com/acme/widget/pull/42",
		State:  contracts.PullRequestOpen.String(),
		Merge:  contracts.MergeStateUnknown.String(),
		Head:   "feat",
		Base:   "master",
	}
	if view.Number != want.Number || view.Title != want.Title || view.URL != want.URL || view.State != want.State || view.Merge != want.Merge || view.Head != want.Head || view.Base != want.Base {
		t.Fatalf("status = %+v, want %+v", view, want)
	}
}

func TestPRStatusInFrenchTranslatesTheWords(t *testing.T) {
	dir := prFixtureRepo(t)
	st := prState(t, dir, "fr")
	if err := st.RecordPullRequest("alpha", state.PullRequestRecord{Number: prTestNumber, State: contracts.PullRequestOpen}); err != nil {
		t.Fatal(err)
	}
	pullRequest := prNumbered(t)
	pullRequest.Merge = contracts.MergeStateClean
	reg := prRegistry(t, st, prForgeOpener(&fakePRForge{pullRequest: pullRequest}))

	out, err := runPR(t, reg, verbPRStatus, "--name", "alpha")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	for _, want := range []string{"ouverte", "propre"} {
		if !strings.Contains(out, want) {
			t.Fatalf("status in French %q does not contain %q", out, want)
		}
	}
	if strings.Contains(out, contracts.PullRequestOpen.String()) {
		t.Fatalf("status in French %q still shows the English state", out)
	}
}

func TestPRChecksInFrenchTranslatesTheConclusions(t *testing.T) {
	dir := prFixtureRepo(t)
	st := prState(t, dir, "fr")
	if err := st.RecordPullRequest("alpha", state.PullRequestRecord{Number: prTestNumber, State: contracts.PullRequestOpen}); err != nil {
		t.Fatal(err)
	}
	forge := &fakePRForge{
		pullRequest: prNumbered(t),
		checkPages: [][]contracts.Check{{
			{Name: "unit", Status: contracts.CheckCompleted, Conclusion: contracts.ConclusionSuccess},
			{Name: "lint", Status: contracts.CheckCompleted, Conclusion: contracts.ConclusionFailure},
		}},
	}
	reg := prRegistry(t, st, prForgeOpener(forge))

	out, err := runPR(t, reg, verbPRChecks, "--name", "alpha")
	if err != nil {
		t.Fatalf("checks: %v", err)
	}
	for _, want := range []string{"réussi unit", "échec lint"} {
		if !strings.Contains(out, want) {
			t.Fatalf("checks in French %q does not contain %q", out, want)
		}
	}
}

func TestPRForgeNotConnectedInFrenchNamesTheRemedy(t *testing.T) {
	dir := prFixtureRepo(t)
	st := prState(t, dir, "fr")
	if err := st.RecordPullRequest("alpha", state.PullRequestRecord{Number: prTestNumber, State: contracts.PullRequestOpen}); err != nil {
		t.Fatal(err)
	}
	reg := prRegistry(t, st, prForgeOpener(&fakePRForge{pullRequest: prNumbered(t), pullErr: contracts.ErrForgeNotConnected}))

	_, err := runPR(t, reg, verbPRStatus, "--name", "alpha")
	if !errors.Is(err, contracts.ErrForgeNotConnected) {
		t.Fatalf("status = %v, want the forge error kept in the chain", err)
	}
	if !strings.Contains(err.Error(), "herrscher connect github") || !strings.Contains(err.Error(), "aucun identifiant") {
		t.Fatalf("error %q is not the French remedy", err)
	}
}

func TestPRWordsTranslateEveryValueAndNameAnUnknownOne(t *testing.T) {
	p := sessionPRs{messages: clitext.NewMessages()}
	for _, language := range []i18n.Language{i18n.English, i18n.French} {
		assertEveryStateWord(t, p, language)
		assertEveryMergeWord(t, p, language)
		assertEveryConclusionWord(t, p, language)
	}
	word, err := p.stateWord(i18n.French, contracts.PullRequestState(99))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(word, "valeur inconnue") || !strings.Contains(word, "99") {
		t.Fatalf("unknown state renders %q, want the translated unknown-value text naming 99", word)
	}
}

func assertEveryStateWord(t *testing.T, p sessionPRs, language i18n.Language) {
	t.Helper()
	for _, value := range []contracts.PullRequestState{contracts.PullRequestOpen, contracts.PullRequestClosed, contracts.PullRequestMerged} {
		if word, err := p.stateWord(language, value); err != nil || word == "" {
			t.Fatalf("stateWord(%v, %v) = %q, %v", language, value, word, err)
		}
	}
}

func assertEveryMergeWord(t *testing.T, p sessionPRs, language i18n.Language) {
	t.Helper()
	for _, value := range []contracts.MergeState{contracts.MergeStateUnknown, contracts.MergeStateClean, contracts.MergeStateConflicting, contracts.MergeStateBlocked, contracts.MergeStateBehind, contracts.MergeStateDraft} {
		if word, err := p.mergeWord(language, value); err != nil || word == "" {
			t.Fatalf("mergeWord(%v, %v) = %q, %v", language, value, word, err)
		}
	}
}

func assertEveryConclusionWord(t *testing.T, p sessionPRs, language i18n.Language) {
	t.Helper()
	for _, value := range []contracts.CheckConclusion{contracts.ConclusionNone, contracts.ConclusionSuccess, contracts.ConclusionFailure, contracts.ConclusionNeutral, contracts.ConclusionCancelled, contracts.ConclusionSkipped, contracts.ConclusionTimedOut, contracts.ConclusionActionRequired, contracts.ConclusionOther} {
		if word, err := p.conclusionWord(language, value); err != nil || word == "" {
			t.Fatalf("conclusionWord(%v, %v) = %q, %v", language, value, word, err)
		}
	}
}
