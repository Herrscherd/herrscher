package forge

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/forgetest"
)

const testToken = "ghp_testsecret123"

func basicTestHeader() string {
	return base64.StdEncoding.EncodeToString([]byte("x-access-token:" + testToken))
}

type fakeForge struct {
	contracts.Forge

	pages   map[contracts.PageCursor]contracts.Page[contracts.ForgeRepo]
	err     error
	cursors []contracts.PageCursor
	pageFn  func(cursor contracts.PageCursor) (contracts.Page[contracts.ForgeRepo], error)
}

func (f *fakeForge) Repositories(_ context.Context, page contracts.PageRequest) (contracts.Page[contracts.ForgeRepo], error) {
	if err := page.Validate(); err != nil {
		return contracts.Page[contracts.ForgeRepo]{}, err
	}
	f.cursors = append(f.cursors, page.Cursor)
	if f.err != nil {
		return contracts.Page[contracts.ForgeRepo]{}, f.err
	}
	if f.pageFn != nil {
		return f.pageFn(page.Cursor)
	}
	return f.pages[page.Cursor], nil
}

type fakeCreds struct {
	secret string
	err    error
	zero   bool
}

func (c fakeCreds) Token(context.Context, contracts.ForgeHost) (contracts.ForgeToken, error) {
	if c.err != nil {
		return contracts.ForgeToken{}, c.err
	}
	if c.zero {
		return contracts.ForgeToken{}, nil
	}
	return contracts.NewForgeToken(c.secret, "octo", contracts.TokenSourceSaved)
}

func (fakeCreds) Invalidate(context.Context, contracts.ForgeHost) {}

type runCall struct {
	dir  string
	env  []string
	name string
	args []string
}

type fakeRunner struct {
	calls  []runCall
	out    []byte
	err    error
	gitHub bool
}

func (r *fakeRunner) look(name string) error {
	if name == "glab" && r.gitHub {
		return nil
	}
	return errors.New("not installed")
}

func (r *fakeRunner) run(_ context.Context, dir string, env []string, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, runCall{dir: dir, env: env, name: name, args: args})
	return r.out, r.err
}

func mustHost(t *testing.T) contracts.ForgeHost {
	t.Helper()
	host, err := contracts.ParseForgeHost("github.com")
	if err != nil {
		t.Fatal(err)
	}
	return host
}

func listedRepo(t *testing.T, repoText string) contracts.ForgeRepo {
	t.Helper()
	ref, err := contracts.ParseRepoRef(repoText)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := contracts.NewForgeRepo(ref.Host(), contracts.ForgeRepoFields{Repo: ref, CloneURL: "https://" + repoText + ".git", Description: "d"})
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

func openWith(forge contracts.Forge) openForge {
	return func(context.Context, contracts.ForgeHost) (contracts.Forge, error) { return forge, nil }
}

func TestListGitHubReadsEveryPageInOrder(t *testing.T) {
	forge := &fakeForge{pages: map[contracts.PageCursor]contracts.Page[contracts.ForgeRepo]{
		"":   {Items: []contracts.ForgeRepo{listedRepo(t, "github.com/a/one")}, Next: "p2"},
		"p2": {Items: []contracts.ForgeRepo{listedRepo(t, "github.com/a/two")}, Next: "p3"},
		"p3": {Items: []contracts.ForgeRepo{listedRepo(t, "github.com/b/three")}},
	}}
	client := newClient(fakeCreds{}, openWith(forge), &fakeRunner{})

	repos, err := client.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range repos {
		names = append(names, r.FullName)
	}
	if !slices.Equal(names, []string{"a/one", "a/two", "b/three"}) {
		t.Fatalf("names = %v", names)
	}
	if !slices.Equal(forge.cursors, []contracts.PageCursor{"", "p2", "p3"}) {
		t.Fatalf("cursors = %v", forge.cursors)
	}
	if repos[0].Forge != "github" || repos[0].Desc != "d" || repos[0].CloneURL != "https://github.com/a/one.git" {
		t.Fatalf("mapped repo = %+v", repos[0])
	}
}

func TestListGitHubStopsAtThePageBound(t *testing.T) {
	forge := &fakeForge{pageFn: func(cursor contracts.PageCursor) (contracts.Page[contracts.ForgeRepo], error) {
		return contracts.Page[contracts.ForgeRepo]{Next: cursor + "x"}, nil
	}}
	client := newClient(fakeCreds{}, openWith(forge), &fakeRunner{})

	_, err := client.List(context.Background())
	if !errors.Is(err, errTooManyPages) {
		t.Fatalf("err = %v, want errTooManyPages", err)
	}
	if len(forge.cursors) != maxListPages {
		t.Fatalf("pages fetched = %d, want %d", len(forge.cursors), maxListPages)
	}
}

func TestListGitHubReturnsAPageError(t *testing.T) {
	boom := errors.New("forge down")
	forge := &fakeForge{pageFn: func(contracts.PageCursor) (contracts.Page[contracts.ForgeRepo], error) {
		return contracts.Page[contracts.ForgeRepo]{}, boom
	}}
	client := newClient(fakeCreds{}, openWith(forge), &fakeRunner{})

	_, err := client.List(context.Background())
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the page error", err)
	}
}

func TestListGitHubWithoutCredentialListsNothing(t *testing.T) {
	forge := &fakeForge{err: contracts.ErrForgeNotConnected}
	client := newClient(fakeCreds{}, openWith(forge), &fakeRunner{})

	repos, err := client.List(context.Background())
	if err != nil || len(repos) != 0 {
		t.Fatalf("List = %v, %v; want no repositories and no error", repos, err)
	}
}

func TestListGitHubPassesTheContractsListingCheck(t *testing.T) {
	forge := &fakeForge{pages: map[contracts.PageCursor]contracts.Page[contracts.ForgeRepo]{
		"":   {Items: []contracts.ForgeRepo{listedRepo(t, "github.com/a/one")}, Next: "p2"},
		"p2": {Items: []contracts.ForgeRepo{listedRepo(t, "github.com/a/two")}},
	}}
	forgetest.CheckListsRepositories(t, forge, mustHost(t))
}

func TestCloneOwnerNameTokenOnlyInEnvironment(t *testing.T) {
	runner := &fakeRunner{}
	client := newClient(fakeCreds{secret: testToken}, nil, runner)

	dir, err := client.Clone(context.Background(), "owner/repo", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	call := runner.calls[0]
	if filepath.Base(dir) != "repo" {
		t.Fatalf("dir = %q", dir)
	}
	if !slices.Equal(call.args, []string{"clone", "--", "https://github.com/owner/repo.git", dir}) {
		t.Fatalf("argv = %v", call.args)
	}
	assertNoSecret(t, call.args, call.dir)
	if !slices.Contains(call.env, "GIT_CONFIG_VALUE_0=Authorization: Basic "+basicTestHeader()) {
		t.Fatalf("env misses the basic auth header: %v", call.env)
	}
	if !slices.Contains(call.env, "GIT_CONFIG_KEY_0=http.https://github.com/.extraheader") || !slices.Contains(call.env, "GIT_TERMINAL_PROMPT=0") {
		t.Fatalf("env = %v", call.env)
	}
}

func TestCloneRefusesBeforeAnyGitCommand(t *testing.T) {
	cases := map[string]string{
		"ssh://github.com/owner/repo":    "ssh URL without the git user",
		"https://github.com/owner/repo?": "query",
		"https://github.com/owner/..":    "dot-dot name",
		"owner/-repo":                    "leading dash",
		"owner/repo.git/..":              "dot-dot segment after the suffix",
		"github.com/owner/repo":          "host-prefixed bare path",
		"owner/repo\n":                   "control character",
		"":                               "empty",
	}
	for spec, why := range cases {
		t.Run(why, func(t *testing.T) {
			runner := &fakeRunner{}
			client := newClient(fakeCreds{secret: testToken}, nil, runner)
			if _, err := client.Clone(context.Background(), spec, t.TempDir()); !errors.Is(err, ErrRepoSpec) {
				t.Fatalf("Clone(%q) err = %v, want ErrRepoSpec", spec, err)
			}
			if len(runner.calls) != 0 {
				t.Fatalf("git ran for %q", spec)
			}
		})
	}
}

func TestCloneAcceptsSSHFormsWithoutToken(t *testing.T) {
	cases := map[string]string{
		"git@github.com:owner/repo.git":       "scp form",
		"ssh://git@github.com/owner/repo.git": "ssh URL",
	}
	for spec, why := range cases {
		t.Run(why, func(t *testing.T) {
			runner := &fakeRunner{}
			client := newClient(fakeCreds{secret: testToken}, nil, runner)
			workspace := t.TempDir()
			dir, err := client.Clone(context.Background(), spec, workspace)
			if err != nil {
				t.Fatal(err)
			}
			call := runner.calls[0]
			if dir != filepath.Join(workspace, "repo") || !slices.Equal(call.args, []string{"clone", "--", spec, dir}) {
				t.Fatalf("dir = %q, argv = %v", dir, call.args)
			}
			assertAnonymous(t, call.env)
		})
	}
}

func TestCloneHttpsGitHubWithoutConnectionIsAnonymous(t *testing.T) {
	runner := &fakeRunner{}
	client := newClient(fakeCreds{err: contracts.ErrForgeNotConnected}, nil, runner)

	dir, err := client.Clone(context.Background(), "https://github.com/owner/repo.git", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	call := runner.calls[0]
	if !slices.Equal(call.args, []string{"clone", "--", "https://github.com/owner/repo.git", dir}) {
		t.Fatalf("argv = %v", call.args)
	}
	assertAnonymous(t, call.env)
}

func TestCloneOwnerNameWithoutConnectionIsAnonymous(t *testing.T) {
	runner := &fakeRunner{}
	client := newClient(fakeCreds{err: contracts.ErrForgeNotConnected}, nil, runner)

	if _, err := client.Clone(context.Background(), "owner/repo", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if runner.calls[0].name != "git" {
		t.Fatalf("command = %q, want git", runner.calls[0].name)
	}
	assertAnonymous(t, runner.calls[0].env)
}

func TestCloneZeroTokenIsAnonymous(t *testing.T) {
	runner := &fakeRunner{}
	client := newClient(fakeCreds{zero: true}, nil, runner)

	if _, err := client.Clone(context.Background(), "https://github.com/owner/repo.git", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	assertAnonymous(t, runner.calls[0].env)
}

func TestCloneTokenNeverGoesToAnotherHost(t *testing.T) {
	cases := map[string]string{
		"https://ghe.example.com/owner/repo": "GitHub Enterprise host",
		"https://gitlab.com/owner/repo.git":  "GitLab host",
		"https://github.com.evil.test/o/r":   "lookalike host",
	}
	for spec, why := range cases {
		t.Run(why, func(t *testing.T) {
			runner := &fakeRunner{}
			client := newClient(fakeCreds{secret: testToken}, nil, runner)

			if _, err := client.Clone(context.Background(), spec, t.TempDir()); err != nil {
				t.Fatal(err)
			}
			call := runner.calls[0]
			assertAnonymous(t, call.env)
			assertNoSecret(t, call.args, call.dir)
		})
	}
}

func TestCloneForgeCredentialFailureStopsBeforeGit(t *testing.T) {
	boom := errors.New("credential store unreadable")
	runner := &fakeRunner{}
	client := newClient(fakeCreds{err: boom}, nil, runner)

	_, err := client.Clone(context.Background(), "https://github.com/owner/repo.git", t.TempDir())
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the credential error", err)
	}
	if len(runner.calls) != 0 {
		t.Fatal("git ran after a credential failure")
	}
}

func TestCloneOwnerNameIsAnonymousGitEvenWithGlabInstalled(t *testing.T) {
	runner := &fakeRunner{gitHub: true}
	client := newClient(fakeCreds{err: contracts.ErrForgeNotConnected}, nil, runner)
	workspace := t.TempDir()

	dir, err := client.Clone(context.Background(), "owner/repo", workspace)
	if err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("calls = %d, want one git clone", len(runner.calls))
	}
	call := runner.calls[0]
	if call.name != "git" || !slices.Equal(call.args, []string{"clone", "--", "https://github.com/owner/repo.git", dir}) {
		t.Fatalf("command = %s %v, want git clone of github.com", call.name, call.args)
	}
	assertAnonymous(t, call.env)
}

func TestCloneOwnerNameWithTokenDoesNotUseGlab(t *testing.T) {
	runner := &fakeRunner{gitHub: true}
	client := newClient(fakeCreds{secret: testToken}, nil, runner)

	if _, err := client.Clone(context.Background(), "owner/repo", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if runner.calls[0].name != "git" {
		t.Fatalf("command = %q, want git", runner.calls[0].name)
	}
}

func TestCloneZeroTokenNeverSendsAuthHeader(t *testing.T) {
	runner := &fakeRunner{}
	client := newClient(fakeCreds{zero: true, secret: testToken}, nil, runner)

	if _, err := client.Clone(context.Background(), "owner/repo", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	assertAnonymous(t, runner.calls[0].env)
}

func TestCloneHttpsURLClonesTheNamedRepository(t *testing.T) {
	runner := &fakeRunner{}
	client := newClient(fakeCreds{secret: testToken}, nil, runner)
	workspace := t.TempDir()

	dir, err := client.Clone(context.Background(), "https://github.com/owner/repo.git", workspace)
	if err != nil {
		t.Fatal(err)
	}
	if dir != filepath.Join(workspace, "repo") || !slices.Contains(runner.calls[0].args, "https://github.com/owner/repo.git") {
		t.Fatalf("dir = %q, argv = %v", dir, runner.calls[0].args)
	}
}

func TestCloneErrorNeverCarriesTheToken(t *testing.T) {
	leaking := []byte("fatal: unable to access: Authorization: Basic " + testToken + " refused")
	runner := &fakeRunner{out: leaking, err: errors.New("exit status 128")}
	client := newClient(fakeCreds{secret: testToken}, nil, runner)

	_, err := client.Clone(context.Background(), "owner/repo", t.TempDir())
	if err == nil {
		t.Fatal("expected a clone error")
	}
	if strings.Contains(err.Error(), testToken) {
		t.Fatalf("error leaks the token: %v", err)
	}
}

func TestCloneErrorNeverCarriesTheBasicAuthHeader(t *testing.T) {
	leaking := []byte("fatal: header " + basicTestHeader() + " refused")
	runner := &fakeRunner{out: leaking, err: errors.New("exit status 128")}
	client := newClient(fakeCreds{secret: testToken}, nil, runner)

	_, err := client.Clone(context.Background(), "owner/repo", t.TempDir())
	if err == nil {
		t.Fatal("expected a clone error")
	}
	if strings.Contains(err.Error(), basicTestHeader()) || strings.Contains(err.Error(), testToken) {
		t.Fatalf("error leaks the credential: %v", err)
	}
}

func TestCloneExistingTargetIsReturnedWithoutReadingTheCredential(t *testing.T) {
	workspace := t.TempDir()
	if err := os.Mkdir(filepath.Join(workspace, "repo"), 0o755); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	client := newClient(fakeCreds{err: errors.New("credential store unreadable")}, nil, runner)

	dir, err := client.Clone(context.Background(), "owner/repo", workspace)
	if err != nil || dir != filepath.Join(workspace, "repo") {
		t.Fatalf("Clone = %q, %v", dir, err)
	}
	if len(runner.calls) != 0 {
		t.Fatal("git ran for an existing target")
	}
}

func TestCloneKeepsTheUsersGitConfigEntries(t *testing.T) {
	t.Setenv(configCount, "2")
	runner := &fakeRunner{}
	client := newClient(fakeCreds{secret: testToken}, nil, runner)

	if _, err := client.Clone(context.Background(), "owner/repo", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	env := runner.calls[0].env
	for _, want := range []string{
		"GIT_CONFIG_COUNT=3",
		"GIT_CONFIG_KEY_2=http.https://github.com/.extraheader",
		"GIT_CONFIG_VALUE_2=Authorization: Basic " + basicTestHeader(),
	} {
		if !slices.Contains(env, want) {
			t.Fatalf("env misses %q: %v", want, env)
		}
	}
	if slices.Contains(env, "GIT_CONFIG_KEY_0=http.https://github.com/.extraheader") {
		t.Fatalf("env reuses index 0 over the user's entries: %v", env)
	}
}

func envWithCount(count string) func(string) string {
	return func(key string) string {
		if key == configCount {
			return count
		}
		return ""
	}
}

func TestGitEnvironmentIndexesFromTheExistingCount(t *testing.T) {
	cases := map[string]struct {
		count string
		index string
		next  string
	}{
		"unset":         {count: "", index: "0", next: "1"},
		"two":           {count: "2", index: "2", next: "3"},
		"largest bound": {count: "1000", index: "1000", next: "1001"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			env, err := gitEnvironment(envWithCount(tc.count), mustHost(t), mustToken(t))
			if err != nil {
				t.Fatal(err)
			}
			wantCount := "GIT_CONFIG_COUNT=" + tc.next
			if !slices.Contains(env, wantCount) || !slices.Contains(env, "GIT_CONFIG_KEY_"+tc.index+"=http.https://github.com/.extraheader") {
				t.Fatalf("env = %v, want %s and index %s", env, wantCount, tc.index)
			}
		})
	}
}

func TestGitEnvironmentRefusesAUnusableCountOnlyWithAToken(t *testing.T) {
	for _, count := range []string{"many", "-1", "1001", "9223372036854775807", "9223372036854775808"} {
		t.Run(count, func(t *testing.T) {
			env, err := gitEnvironment(envWithCount(count), mustHost(t), contracts.ForgeToken{})
			if err != nil || len(env) != 1 {
				t.Fatalf("anonymous env = %v, %v; the count must not be read", env, err)
			}
			if _, err := gitEnvironment(envWithCount(count), mustHost(t), mustToken(t)); !errors.Is(err, ErrConfigCount) {
				t.Fatalf("err = %v, want ErrConfigCount", err)
			}
		})
	}
}

func TestCloneRefusesAnUnusableCountBeforeGit(t *testing.T) {
	t.Setenv(configCount, "9223372036854775807")
	runner := &fakeRunner{}
	client := newClient(fakeCreds{secret: testToken}, nil, runner)

	if _, err := client.Clone(context.Background(), "owner/repo", t.TempDir()); !errors.Is(err, ErrConfigCount) {
		t.Fatalf("err = %v, want ErrConfigCount", err)
	}
	if len(runner.calls) != 0 {
		t.Fatal("git ran with an unusable GIT_CONFIG_COUNT")
	}
}

func TestCloneErrorNeverCarriesUserinfoFromAnInvalidSpec(t *testing.T) {
	const secret = "SUPERSECRET"
	cases := []string{
		"https://x-access-token:" + secret + "@github.com/owner/repo.git/..",
		"https://x-access-token:" + secret + "@github.com/owner/repo?token=" + secret,
		"ssh://x-access-token:" + secret + "@github.com/owner/repo\n",
		"https://x-access-token:" + secret + "@gith\nub.com/owner/repo",
		"x-access-token:" + secret + "@github.com/owner/repo",
	}
	for _, spec := range cases {
		t.Run(spec, func(t *testing.T) {
			runner := &fakeRunner{}
			client := newClient(fakeCreds{secret: testToken}, nil, runner)

			_, err := client.Clone(context.Background(), spec, t.TempDir())
			if !errors.Is(err, ErrRepoSpec) {
				t.Fatalf("err = %v, want ErrRepoSpec", err)
			}
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("error leaks the userinfo: %v", err)
			}
			if len(runner.calls) != 0 {
				t.Fatal("git ran for an invalid spec")
			}
		})
	}
}

func mustToken(t *testing.T) contracts.ForgeToken {
	t.Helper()
	token, err := contracts.NewForgeToken(testToken, "octo", contracts.TokenSourceSaved)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func TestListIncludesGitLabOnlyWhenGlabIsInstalled(t *testing.T) {
	forge := &fakeForge{}
	runner := &fakeRunner{gitHub: true, out: []byte(`[{"path_with_namespace":"g/p","http_url_to_repo":"https://gitlab.com/g/p.git","ssh_url_to_repo":"git@gitlab.com:g/p.git","description":"x"}]`)}
	client := newClient(fakeCreds{}, openWith(forge), runner)

	repos, err := client.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 || repos[0].Forge != "gitlab" || repos[0].FullName != "g/p" || repos[0].CloneURL != "git@gitlab.com:g/p.git" {
		t.Fatalf("repos = %+v", repos)
	}
}

func TestGitLabListingEntryClonesFromItsOwnHost(t *testing.T) {
	runner := &fakeRunner{gitHub: true, out: []byte(`[{"path_with_namespace":"group/sub/project","ssh_url_to_repo":"git@gitlab.com:group/sub/project.git","http_url_to_repo":"https://gitlab.com/group/sub/project.git","description":"x"}]`)}
	client := newClient(fakeCreds{secret: testToken}, openWith(&fakeForge{}), runner)
	repos, err := client.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	runner.calls = nil
	workspace := t.TempDir()

	dir, err := client.Clone(context.Background(), repos[0].Spec(), workspace)
	if err != nil {
		t.Fatal(err)
	}
	call := runner.calls[0]
	if dir != filepath.Join(workspace, "project") || !slices.Equal(call.args, []string{"clone", "--", "git@gitlab.com:group/sub/project.git", dir}) {
		t.Fatalf("dir = %q, argv = %v", dir, call.args)
	}
	assertAnonymous(t, call.env)
	if strings.Contains(strings.Join(call.args, " "), "github.com") {
		t.Fatalf("GitLab entry cloned from github.com: %v", call.args)
	}
}

func TestGitLabListingFallsBackToHTTPSOnlyWithoutSSHURL(t *testing.T) {
	runner := &fakeRunner{gitHub: true, out: []byte(`[{"path_with_namespace":"g/p","ssh_url_to_repo":"","http_url_to_repo":"https://gitlab.example.com/g/p.git","description":"x"}]`)}
	client := newClient(fakeCreds{secret: testToken}, openWith(&fakeForge{}), runner)
	repos, err := client.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if repos[0].Spec() != "https://gitlab.example.com/g/p.git" {
		t.Fatalf("Spec() = %q, want the https URL", repos[0].Spec())
	}
	runner.calls = nil
	workspace := t.TempDir()

	dir, err := client.Clone(context.Background(), repos[0].Spec(), workspace)
	if err != nil {
		t.Fatal(err)
	}
	call := runner.calls[0]
	if dir != filepath.Join(workspace, "p") || !slices.Equal(call.args, []string{"clone", "--", "https://gitlab.example.com/g/p.git", dir}) {
		t.Fatalf("dir = %q, argv = %v", dir, call.args)
	}
	assertAnonymous(t, call.env)
}

func TestGitLabListingWithoutAUsableURLHasNoSpec(t *testing.T) {
	cases := map[string]string{
		"no URL":          `{"path_with_namespace":"g/p","ssh_url_to_repo":"","http_url_to_repo":""}`,
		"plain http only": `{"path_with_namespace":"g/p","ssh_url_to_repo":"","http_url_to_repo":"http://gitlab.example.com/g/p.git"}`,
	}
	for why, item := range cases {
		t.Run(why, func(t *testing.T) {
			runner := &fakeRunner{gitHub: true, out: []byte("[" + item + "]")}
			client := newClient(fakeCreds{}, openWith(&fakeForge{}), runner)
			repos, err := client.List(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if spec := repos[0].Spec(); spec != "" {
				t.Fatalf("Spec() = %q, want empty", spec)
			}
		})
	}
}

func TestCloneGitLabSSHFormsOnAnyHostAreAnonymous(t *testing.T) {
	cases := map[string]string{
		"git@gitlab.com:group/project.git":                    "hosted scp form",
		"git@gitlab.example.com:group/project.git":            "self-hosted scp form",
		"ssh://git@gitlab.example.com:2222/group/project.git": "self-hosted ssh URL with a port",
		"git@10.0.0.5:group/project.git":                      "self-hosted scp form on an address",
	}
	for spec, why := range cases {
		t.Run(why, func(t *testing.T) {
			runner := &fakeRunner{}
			client := newClient(fakeCreds{secret: testToken}, nil, runner)
			workspace := t.TempDir()

			dir, err := client.Clone(context.Background(), spec, workspace)
			if err != nil {
				t.Fatal(err)
			}
			call := runner.calls[0]
			if call.name != "git" || dir != filepath.Join(workspace, "project") || !slices.Equal(call.args, []string{"clone", "--", spec, dir}) {
				t.Fatalf("command = %s %v, dir = %q", call.name, call.args, dir)
			}
			assertAnonymous(t, call.env)
			assertNoSecret(t, call.args, call.dir)
		})
	}
}

func TestGitHubListingEntryStillClonesFromGitHubWithToken(t *testing.T) {
	forge := &fakeForge{pages: map[contracts.PageCursor]contracts.Page[contracts.ForgeRepo]{
		"": {Items: []contracts.ForgeRepo{listedRepo(t, "github.com/a/one")}},
	}}
	runner := &fakeRunner{}
	client := newClient(fakeCreds{secret: testToken}, openWith(forge), runner)
	repos, err := client.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if repos[0].Spec() != "a/one" {
		t.Fatalf("Spec() = %q, want a/one", repos[0].Spec())
	}
	workspace := t.TempDir()

	dir, err := client.Clone(context.Background(), repos[0].Spec(), workspace)
	if err != nil {
		t.Fatal(err)
	}
	call := runner.calls[0]
	if dir != filepath.Join(workspace, "one") || !slices.Equal(call.args, []string{"clone", "--", "https://github.com/a/one.git", dir}) {
		t.Fatalf("dir = %q, argv = %v", dir, call.args)
	}
	if !slices.Contains(call.env, "GIT_CONFIG_VALUE_0=Authorization: Basic "+basicTestHeader()) {
		t.Fatalf("GitHub clone misses the token header: %v", call.env)
	}
}

func TestNewWithoutCredentialsHoldsNoToken(t *testing.T) {
	client := New(nil)

	_, err := client.creds.Token(context.Background(), mustHost(t))
	if !errors.Is(err, contracts.ErrForgeNotConnected) {
		t.Fatalf("Token err = %v, want ErrForgeNotConnected", err)
	}
}

func TestListFailsWhenTheForgeCannotBeOpened(t *testing.T) {
	boom := fmt.Errorf("plugin: %w", contracts.ErrInvalidForgeHost)
	client := newClient(fakeCreds{}, func(context.Context, contracts.ForgeHost) (contracts.Forge, error) { return nil, boom }, &fakeRunner{})

	if _, err := client.List(context.Background()); !errors.Is(err, contracts.ErrInvalidForgeHost) {
		t.Fatalf("err = %v", err)
	}
}

func assertNoSecret(t *testing.T, args []string, dir string) {
	t.Helper()
	for _, arg := range args {
		if strings.Contains(arg, testToken) {
			t.Fatalf("token on argv: %q", arg)
		}
	}
	if strings.Contains(dir, testToken) {
		t.Fatal("token in working directory")
	}
}

func assertAnonymous(t *testing.T, env []string) {
	t.Helper()
	for _, entry := range env {
		if strings.HasPrefix(entry, "GIT_CONFIG_") || strings.Contains(entry, basicTestHeader()) || strings.Contains(entry, testToken) {
			t.Fatalf("anonymous clone carries a credential: %v", env)
		}
	}
}
