// Package forge lists the repositories of the forge hosts a herrscher host is
// connected to and clones them. GitHub goes through the contracts forge port,
// with its token passed to git only through the environment; GitLab listing
// stays on the glab command line.
package forge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/forgeport"
	"github.com/Herrscherd/herrscher/core/internal/redact"
)

const (
	githubLabel  = "github"
	gitlabLabel  = "gitlab"
	maxListPages = 20
	tokenUser    = "x-access-token"
	subRepo      = "repo"
	subList      = "list"
	redactedMark = "[redacted]"
)

var (
	errRepoSpec     = errors.New("invalid repo spec")
	errTooManyPages = errors.New("repository listing has more pages than the bound")
	specRe          = regexp.MustCompile(`^[\w.\-]+/[\w.\-]+$`)
)

// Repo is one remote repository discovered on a forge host.
type Repo struct {
	FullName string
	CloneURL string
	Desc     string
	Forge    string
}

type runner interface {
	look(name string) error
	run(ctx context.Context, dir string, env []string, name string, args ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) look(name string) error { _, err := exec.LookPath(name); return err }

func (execRunner) run(ctx context.Context, dir string, env []string, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if env != nil {
		cmd.Env = append(os.Environ(), env...)
	}
	return cmd.CombinedOutput()
}

type openForge func(ctx context.Context, host contracts.ForgeHost) (contracts.Forge, error)

// Client lists and clones repositories for the host.
type Client struct {
	creds contracts.ForgeCredentials
	open  openForge
	r     runner
}

// New returns a Client that reaches GitHub through the forge plugin, with
// creds as the port that supplies its token.
func New(creds contracts.ForgeCredentials) *Client {
	open := func(ctx context.Context, host contracts.ForgeHost) (contracts.Forge, error) {
		return forgeport.Open(ctx, forgeport.KindGitHub, host, creds)
	}
	return newClient(creds, open, execRunner{})
}

func newClient(creds contracts.ForgeCredentials, open openForge, r runner) *Client {
	return &Client{creds: creds, open: open, r: r}
}

// List returns the repositories of GitHub, when the host holds a credential for
// it, and of GitLab, when glab is installed. A host without a GitHub credential
// lists no GitHub repository and no error.
func (c *Client) List(ctx context.Context) ([]Repo, error) {
	out, err := c.listGitHub(ctx)
	if err != nil {
		return nil, err
	}
	if c.r.look("glab") == nil {
		gitlab, err := c.listGitLab(ctx)
		if err != nil {
			return nil, err
		}
		out = append(out, gitlab...)
	}
	return out, nil
}

func (c *Client) listGitHub(ctx context.Context) ([]Repo, error) {
	host, err := contracts.ParseForgeHost(forgeport.GitHubHost)
	if err != nil {
		return nil, err
	}
	forge, err := c.open(ctx, host)
	if err != nil {
		return nil, notConnectedAsEmpty(err, "github open")
	}
	var out []Repo
	var cursor contracts.PageCursor
	for fetched := 1; ; fetched++ {
		page, err := forge.Repositories(ctx, contracts.PageRequest{Cursor: cursor, Limit: contracts.MaxPageLimit})
		if err != nil {
			return nil, notConnectedAsEmpty(err, "github repositories")
		}
		out = append(out, githubRepos(page.Items)...)
		if page.Next == "" {
			return out, nil
		}
		if fetched == maxListPages {
			return nil, fmt.Errorf("github repositories: %w after %d pages", errTooManyPages, maxListPages)
		}
		cursor = page.Next
	}
}

func notConnectedAsEmpty(err error, label string) error {
	if errors.Is(err, contracts.ErrForgeNotConnected) {
		return nil
	}
	return fmt.Errorf("%s: %w", label, err)
}

func githubRepos(items []contracts.ForgeRepo) []Repo {
	out := make([]Repo, 0, len(items))
	for _, item := range items {
		ref := item.Repo()
		out = append(out, Repo{
			FullName: ref.Namespace() + "/" + ref.Name(),
			CloneURL: item.CloneURL(),
			Desc:     item.Description(),
			Forge:    githubLabel,
		})
	}
	return out
}

func (c *Client) listGitLab(ctx context.Context) ([]Repo, error) {
	raw, err := c.r.run(ctx, "", nil, "glab", subRepo, subList, "--output", "json")
	if err != nil {
		return nil, fmt.Errorf("glab repo list: %s: %w", redact.Output(raw), err)
	}
	var items []struct {
		PathWithNamespace string `json:"path_with_namespace"`
		SSHURLToRepo      string `json:"ssh_url_to_repo"`
		Description       string `json:"description"`
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("glab repo list: parse: %w", err)
	}
	out := make([]Repo, 0, len(items))
	for _, it := range items {
		out = append(out, Repo{FullName: it.PathWithNamespace, CloneURL: it.SSHURLToRepo, Desc: it.Description, Forge: gitlabLabel})
	}
	return out, nil
}

// Clone clones spec into workspace and returns the project dir. spec is
// owner/name on GitHub or an https URL of a repository on a forge host; the
// clone URL is validated by the forge port before git runs. An existing target
// is returned as-is.
func (c *Client) Clone(ctx context.Context, spec, workspace string) (string, error) {
	repo, err := cloneRepo(spec)
	if err != nil {
		return "", err
	}
	host := repo.Repo().Host()
	token, err := c.creds.Token(ctx, host)
	if err != nil {
		return "", fmt.Errorf("clone %s: %w", repo.Repo().String(), err)
	}
	if !token.Valid() {
		return "", fmt.Errorf("clone %s: %w", repo.Repo().String(), contracts.ErrForgeNotConnected)
	}
	dir := filepath.Join(workspace, repo.Repo().Name())
	out, err := c.r.run(ctx, workspace, cloneEnv(host, token.Secret()), "git", "clone", "--", repo.CloneURL(), dir)
	if err != nil {
		if strings.Contains(string(out), "already exists") {
			return dir, nil
		}
		return "", fmt.Errorf("git clone: %s: %w", redact.Output([]byte(strings.ReplaceAll(string(out), token.Secret(), redactedMark))), err)
	}
	return dir, nil
}

func cloneRepo(spec string) (contracts.ForgeRepo, error) {
	ref, cloneURL, err := cloneTarget(spec)
	if err != nil {
		return contracts.ForgeRepo{}, err
	}
	repo, err := contracts.NewForgeRepo(ref.Host(), contracts.ForgeRepoFields{Repo: ref, CloneURL: cloneURL})
	if err != nil {
		return contracts.ForgeRepo{}, fmt.Errorf("%w: %q", errRepoSpec, spec)
	}
	return repo, nil
}

func cloneTarget(spec string) (contracts.ForgeRepoRef, string, error) {
	if strings.HasPrefix(spec, "https://") {
		ref, err := contracts.ParseRepoRef(spec)
		if err != nil || ref.Host().String() != forgeport.GitHubHost {
			return contracts.ForgeRepoRef{}, "", fmt.Errorf("%w: %q", errRepoSpec, spec)
		}
		return ref, spec, nil
	}
	if !specRe.MatchString(spec) {
		return contracts.ForgeRepoRef{}, "", fmt.Errorf("%w %q: use owner/name or an https URL", errRepoSpec, spec)
	}
	cloneURL := "https://" + forgeport.GitHubHost + "/" + spec + ".git"
	ref, err := contracts.ParseRepoRef(forgeport.GitHubHost + "/" + spec)
	if err != nil {
		return contracts.ForgeRepoRef{}, "", fmt.Errorf("%w %q", errRepoSpec, spec)
	}
	return ref, cloneURL, nil
}

func cloneEnv(host contracts.ForgeHost, token string) []string {
	basic := base64.StdEncoding.EncodeToString([]byte(tokenUser + ":" + token))
	return []string{
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=http.https://" + host.String() + "/.extraheader",
		"GIT_CONFIG_VALUE_0=Authorization: Basic " + basic,
	}
}
