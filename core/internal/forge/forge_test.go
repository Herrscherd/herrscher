package forge

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeRunner records calls and returns scripted output/errors keyed by argv[0],
// or by the full "name arg arg…" line when outByArgs has a matching entry (used
// to distinguish e.g. `gh repo list` from `gh api user/orgs`).
type fakeRunner struct {
	calls     [][]string
	envs      [][]string        // extra env passed per call, aligned with calls
	out       map[string][]byte // keyed by first arg (e.g. "gh", "glab", "git")
	outByArgs map[string][]byte // keyed by the full joined argv, takes precedence
	err       map[string]error
	lookErr   map[string]error // exec.LookPath result per binary
}

func (f *fakeRunner) look(name string) error {
	if f.lookErr == nil {
		return nil
	}
	return f.lookErr[name]
}

func (f *fakeRunner) run(ctx context.Context, dir string, env []string, name string, args ...string) ([]byte, error) {
	argv := append([]string{name}, args...)
	f.calls = append(f.calls, argv)
	f.envs = append(f.envs, env)
	if f.outByArgs != nil {
		if out, ok := f.outByArgs[strings.Join(argv, " ")]; ok {
			return out, f.err[name]
		}
	}
	return f.out[name], f.err[name]
}

// lastEnv returns the extra env of the most recent call.
func (f *fakeRunner) lastEnv() []string { return f.envs[len(f.envs)-1] }

func hasEnv(env []string, key string) (string, bool) {
	for _, kv := range env {
		if strings.HasPrefix(kv, key+"=") {
			return strings.TrimPrefix(kv, key+"="), true
		}
	}
	return "", false
}

func TestCloneURLWithTokenUsesAskpass(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "ghp_secrettoken")
	t.Setenv("GH_TOKEN", "")
	r := &fakeRunner{}
	c := &Client{r: r}
	if _, err := c.Clone(context.Background(), "https://github.com/me/priv.git", "/ws"); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	env := r.lastEnv()
	if v, ok := hasEnv(env, "GIT_ASKPASS"); !ok || v == "" {
		t.Fatalf("expected GIT_ASKPASS set to self, env=%v", env)
	}
	if v, ok := hasEnv(env, "HERRSCHER_GIT_ASKPASS"); !ok || v != "1" {
		t.Fatalf("expected HERRSCHER_GIT_ASKPASS=1, env=%v", env)
	}
	if v, ok := hasEnv(env, "GIT_TERMINAL_PROMPT"); !ok || v != "0" {
		t.Fatalf("expected GIT_TERMINAL_PROMPT=0, env=%v", env)
	}
	// The token must never leak into argv (visible in `ps`).
	if joined := strings.Join(r.calls[0], " "); strings.Contains(joined, "ghp_secrettoken") {
		t.Fatalf("token leaked into argv: %s", joined)
	}
}

func TestCloneURLWithoutTokenFailsFastNoAskpass(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	r := &fakeRunner{}
	c := &Client{r: r}
	if _, err := c.Clone(context.Background(), "https://github.com/me/pub.git", "/ws"); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	env := r.lastEnv()
	// No token → no askpass, but still fail-fast instead of hanging on a prompt.
	if _, ok := hasEnv(env, "GIT_ASKPASS"); ok {
		t.Fatalf("did not expect GIT_ASKPASS without a token, env=%v", env)
	}
	if v, ok := hasEnv(env, "GIT_TERMINAL_PROMPT"); !ok || v != "0" {
		t.Fatalf("expected GIT_TERMINAL_PROMPT=0 even without token, env=%v", env)
	}
}

func TestCloneOwnerNameNoGitEnv(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "ghp_secrettoken")
	r := &fakeRunner{} // gh present
	c := &Client{r: r}
	if _, err := c.Clone(context.Background(), "me/app", "/ws"); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	// gh reads GH_TOKEN itself; we don't build a git askpass env for its path.
	if _, ok := hasEnv(r.lastEnv(), "GIT_ASKPASS"); ok {
		t.Fatalf("gh path must not set GIT_ASKPASS, env=%v", r.lastEnv())
	}
}

func TestAvailableReportsBothAbsent(t *testing.T) {
	r := &fakeRunner{lookErr: map[string]error{"gh": errors.New("nope"), "glab": errors.New("nope")}}
	c := &Client{r: r}
	gh, gl := c.Available()
	if gh || gl {
		t.Fatalf("expected both absent, got gh=%v gl=%v", gh, gl)
	}
}

func TestListMergesGitHubOnly(t *testing.T) {
	r := &fakeRunner{
		lookErr: map[string]error{"glab": errors.New("nope")}, // only gh present
		out: map[string][]byte{
			"gh": []byte(`[{"nameWithOwner":"me/app","sshUrl":"git@github.com:me/app.git","description":"d"}]`),
		},
	}
	c := &Client{r: r}
	repos, err := c.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(repos) != 1 || repos[0].FullName != "me/app" || repos[0].Forge != "github" {
		t.Fatalf("unexpected repos: %+v", repos)
	}
}

func TestListGitHubIncludesOrgs(t *testing.T) {
	const flags = "--json nameWithOwner,sshUrl,description --limit 100"
	r := &fakeRunner{
		lookErr: map[string]error{"glab": errors.New("nope")}, // only gh present
		outByArgs: map[string][]byte{
			"gh repo list " + flags:                      []byte(`[{"nameWithOwner":"me/app","sshUrl":"git@github.com:me/app.git"}]`),
			"gh api --paginate user/orgs --jq .[].login": []byte("acme\nwidgets\n"),
			"gh repo list acme " + flags:                 []byte(`[{"nameWithOwner":"acme/api","sshUrl":"u"},{"nameWithOwner":"me/app","sshUrl":"u"}]`),
			"gh repo list widgets " + flags:              []byte(`[{"nameWithOwner":"widgets/web","sshUrl":"u"}]`),
		},
	}
	c := &Client{r: r}
	repos, err := c.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	got := map[string]bool{}
	for _, rp := range repos {
		got[rp.FullName] = true
	}
	for _, want := range []string{"me/app", "acme/api", "widgets/web"} {
		if !got[want] {
			t.Fatalf("expected %q in repos, got %+v", want, repos)
		}
	}
	// me/app appears in both personal and acme listings but must be deduped.
	if len(repos) != 3 {
		t.Fatalf("expected 3 deduped repos, got %d: %+v", len(repos), repos)
	}
}

// A failing org enumeration must not drop the user's personal repos.
func TestListGitHubOrgFailureNonFatal(t *testing.T) {
	r := &fakeRunner{
		lookErr: map[string]error{"glab": errors.New("nope")},
		out: map[string][]byte{
			"gh": []byte(`[{"nameWithOwner":"me/app","sshUrl":"u"}]`),
		},
		err: map[string]error{}, // gh api user/orgs returns the repo JSON; treated as junk org, deduped away
	}
	c := &Client{r: r}
	repos, err := c.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(repos) != 1 || repos[0].FullName != "me/app" {
		t.Fatalf("expected just personal repo, got %+v", repos)
	}
}

func TestListEmptyWhenNoForge(t *testing.T) {
	r := &fakeRunner{lookErr: map[string]error{"gh": errors.New("x"), "glab": errors.New("x")}}
	c := &Client{r: r}
	repos, err := c.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(repos) != 0 {
		t.Fatalf("expected no repos, got %+v", repos)
	}
}

func TestCloneRejectsBadSpec(t *testing.T) {
	c := &Client{r: &fakeRunner{}}
	if _, err := c.Clone(context.Background(), "../evil", "/ws"); err == nil {
		t.Fatal("expected rejection of traversal spec")
	}
	if _, err := c.Clone(context.Background(), "a b; rm -rf", "/ws"); err == nil {
		t.Fatal("expected rejection of spec with shell metacharacters")
	}
	// Argument injection: a leading-dash segment could be read as a CLI flag
	// (the git-clone --upload-pack RCE class).
	for _, bad := range []string{"-x/app", "owner/-rf", "--upload-pack=x/app", "ow\tner/app", ""} {
		if _, err := c.Clone(context.Background(), bad, "/ws"); err == nil {
			t.Fatalf("expected rejection of injection spec %q", bad)
		}
	}
}

func TestCloneArgvSeparatesSpec(t *testing.T) {
	// git/glab clones must place spec after a `--` separator (defense in depth).
	r := &fakeRunner{}
	c := &Client{r: r}
	if _, err := c.Clone(context.Background(), "https://github.com/me/app.git", "/ws"); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	argv := r.calls[0]
	sep, spec := -1, -1
	for i, a := range argv {
		if a == "--" {
			sep = i
		}
		if a == "https://github.com/me/app.git" {
			spec = i
		}
	}
	if sep == -1 || spec == -1 || sep > spec {
		t.Fatalf("expected `--` before spec in argv, got %+v", argv)
	}
}

func TestCloneOwnerNameUsesGh(t *testing.T) {
	r := &fakeRunner{} // gh present (no lookErr)
	c := &Client{r: r}
	dir, err := c.Clone(context.Background(), "me/app", "/ws")
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if dir != "/ws/app" {
		t.Fatalf("dir = %q, want /ws/app", dir)
	}
	if len(r.calls) != 1 || r.calls[0][0] != "gh" || !strings.Contains(strings.Join(r.calls[0], " "), "me/app") {
		t.Fatalf("expected gh clone call, got %+v", r.calls)
	}
}

func TestCloneFullURLUsesGit(t *testing.T) {
	r := &fakeRunner{}
	c := &Client{r: r}
	dir, err := c.Clone(context.Background(), "https://github.com/me/app.git", "/ws")
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if dir != "/ws/app" {
		t.Fatalf("dir = %q, want /ws/app", dir)
	}
	if r.calls[0][0] != "git" {
		t.Fatalf("expected git clone, got %+v", r.calls)
	}
}
