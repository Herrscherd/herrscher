package forge

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/forgetest"
)

const testToken = "ghp_testsecret123"

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
}

func (c fakeCreds) Token(context.Context, contracts.ForgeHost) (contracts.ForgeToken, error) {
	if c.err != nil {
		return contracts.ForgeToken{}, c.err
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

	dir, err := client.Clone(context.Background(), "owner/repo", "/ws")
	if err != nil {
		t.Fatal(err)
	}
	if dir != "/ws/repo" {
		t.Fatalf("dir = %q", dir)
	}
	call := runner.calls[0]
	if !slices.Equal(call.args, []string{"clone", "--", "https://github.com/owner/repo.git", "/ws/repo"}) {
		t.Fatalf("argv = %v", call.args)
	}
	assertNoSecret(t, call.args, call.dir)
	wantBasic := "GIT_CONFIG_VALUE_0=Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+testToken))
	if !slices.Contains(call.env, wantBasic) {
		t.Fatalf("env misses the basic auth header: %v", call.env)
	}
	if !slices.Contains(call.env, "GIT_CONFIG_KEY_0=http.https://github.com/.extraheader") || !slices.Contains(call.env, "GIT_TERMINAL_PROMPT=0") {
		t.Fatalf("env = %v", call.env)
	}
}

func TestCloneURLIsCheckedByTheContractsPort(t *testing.T) {
	cases := map[string]string{
		"git@github.com:owner/repo.git":  "scp form",
		"ssh://github.com/owner/repo":    "ssh URL",
		"https://github.com/owner/repo?": "query",
		"https://github.com/owner/..":    "dot-dot name",
		"owner/-repo":                    "leading dash",
		"github.com/owner/repo":          "host-prefixed bare path",
	}
	for spec, why := range cases {
		t.Run(why, func(t *testing.T) {
			runner := &fakeRunner{}
			client := newClient(fakeCreds{secret: testToken}, nil, runner)
			if _, err := client.Clone(context.Background(), spec, "/ws"); !errors.Is(err, errRepoSpec) {
				t.Fatalf("Clone(%q) err = %v, want errRepoSpec", spec, err)
			}
			if len(runner.calls) != 0 {
				t.Fatalf("git ran for %q", spec)
			}
		})
	}
}

func TestCloneHttpsURLClonesTheNamedRepository(t *testing.T) {
	runner := &fakeRunner{}
	client := newClient(fakeCreds{secret: testToken}, nil, runner)

	dir, err := client.Clone(context.Background(), "https://github.com/owner/repo.git", "/ws")
	if err != nil {
		t.Fatal(err)
	}
	if dir != "/ws/repo" || !slices.Contains(runner.calls[0].args, "https://github.com/owner/repo.git") {
		t.Fatalf("dir = %q, argv = %v", dir, runner.calls[0].args)
	}
}

func TestCloneWithoutCredentialStopsBeforeGit(t *testing.T) {
	runner := &fakeRunner{}
	client := newClient(fakeCreds{err: contracts.ErrForgeNotConnected}, nil, runner)

	_, err := client.Clone(context.Background(), "owner/repo", "/ws")
	if !errors.Is(err, contracts.ErrForgeNotConnected) {
		t.Fatalf("err = %v, want ErrForgeNotConnected", err)
	}
	if len(runner.calls) != 0 {
		t.Fatal("git ran without a credential")
	}
}

func TestCloneErrorNeverCarriesTheToken(t *testing.T) {
	leaking := []byte("fatal: unable to access: Authorization: Basic " + testToken + " refused")
	runner := &fakeRunner{out: leaking, err: errors.New("exit status 128")}
	client := newClient(fakeCreds{secret: testToken}, nil, runner)

	_, err := client.Clone(context.Background(), "owner/repo", "/ws")
	if err == nil {
		t.Fatal("expected a clone error")
	}
	if strings.Contains(err.Error(), testToken) {
		t.Fatalf("error leaks the token: %v", err)
	}
}

func TestCloneExistingTargetIsReturnedAsIs(t *testing.T) {
	runner := &fakeRunner{out: []byte("fatal: destination path '/ws/repo' already exists"), err: errors.New("exit status 128")}
	client := newClient(fakeCreds{secret: testToken}, nil, runner)

	dir, err := client.Clone(context.Background(), "owner/repo", "/ws")
	if err != nil || dir != "/ws/repo" {
		t.Fatalf("Clone = %q, %v", dir, err)
	}
}

func TestListIncludesGitLabOnlyWhenGlabIsInstalled(t *testing.T) {
	forge := &fakeForge{}
	runner := &fakeRunner{gitHub: true, out: []byte(`[{"path_with_namespace":"g/p","ssh_url_to_repo":"git@gitlab.com:g/p.git","description":"x"}]`)}
	client := newClient(fakeCreds{}, openWith(forge), runner)

	repos, err := client.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 || repos[0].Forge != "gitlab" || repos[0].FullName != "g/p" {
		t.Fatalf("repos = %+v", repos)
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
