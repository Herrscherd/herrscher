// Package forge lists the repositories of the forge hosts a herrscher host is
// connected to and clones them. GitHub goes through the contracts forge port.
// Its token reaches git only through the environment, and only for github.com
// over https; every other clone runs without it. GitLab listing stays on the
// glab command line.
package forge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/forgeport"
	"github.com/Herrscherd/herrscher/core/internal/redact"
)

const (
	githubLabel  = "github"
	gitlabLabel  = "gitlab"
	tokenUser    = "x-access-token"
	subRepo      = "repo"
	subList      = "list"
	subClone     = "clone"
	redactedMark = "[redacted]"
	configCount  = "GIT_CONFIG_COUNT"
	maxConfigs   = 1000
)

// ErrRepoSpec matches a clone spec that names no repository the clone can reach.
var ErrRepoSpec = errors.New("invalid repo spec")

// ErrConfigCount matches a GIT_CONFIG_COUNT that is not a count from 0 to 1000.
var ErrConfigCount = errors.New(configCount + " is not a count from 0 to " + strconv.Itoa(maxConfigs))

// MaxPages bounds how many pages CollectPages fetches before it stops.
const MaxPages = 20

// ErrTooManyPages matches a listing that still has a next page after MaxPages pages.
var ErrTooManyPages = errors.New("listing has more pages than the bound")

var specRe = regexp.MustCompile(`^[\w.\-]+/[\w.\-]+$`)

// Repo is one remote repository discovered on a forge host.
type Repo struct {
	FullName string
	CloneURL string
	Desc     string
	Forge    string
}

// Spec is the clone spec that reaches the repository on its own host. A GitHub
// repository is named owner/name, which the clone resolves to github.com; any
// other forge is named by the clone URL it lists, which is empty when it lists
// none.
func (r Repo) Spec() string {
	if r.Forge == githubLabel {
		return r.FullName
	}
	return r.CloneURL
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
// creds as the port that supplies its token. A nil creds holds no token: the
// client lists no GitHub repository and clones anonymously.
func New(creds contracts.ForgeCredentials) *Client {
	if creds == nil {
		creds = unconnected{}
	}
	open := func(ctx context.Context, host contracts.ForgeHost) (contracts.Forge, error) {
		return forgeport.Open(ctx, forgeport.KindGitHub, host, creds)
	}
	return newClient(creds, open, execRunner{})
}

type unconnected struct{}

func (unconnected) Token(context.Context, contracts.ForgeHost) (contracts.ForgeToken, error) {
	return contracts.ForgeToken{}, contracts.ErrForgeNotConnected
}

func (unconnected) Invalidate(context.Context, contracts.ForgeHost) {}

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
	items, err := CollectPages(ctx, forge.Repositories)
	if err != nil {
		return nil, notConnectedAsEmpty(err, "github repositories")
	}
	return githubRepos(items), nil
}

// CollectPages fetches every page of a listing, following each cursor from the
// first page, and returns the items in order. It stops with an error wrapping
// ErrTooManyPages when a MaxPages-th page still names a next one.
func CollectPages[T any](ctx context.Context, fetch func(context.Context, contracts.PageRequest) (contracts.Page[T], error)) ([]T, error) {
	var items []T
	var cursor contracts.PageCursor
	for fetched := 1; ; fetched++ {
		page, err := fetch(ctx, contracts.PageRequest{Cursor: cursor, Limit: contracts.MaxPageLimit})
		if err != nil {
			return nil, err
		}
		items = append(items, page.Items...)
		if page.Next == "" {
			return items, nil
		}
		if fetched == MaxPages {
			return nil, fmt.Errorf("%w after %d pages", ErrTooManyPages, MaxPages)
		}
		cursor = page.Next
	}
}

// CollectUpTo fetches the pages of a listing until it holds limit items or the
// listing ends, and reports whether items were left out. It never fetches more
// than MaxPages pages.
func CollectUpTo[T any](ctx context.Context, limit int, fetch func(context.Context, contracts.PageRequest) (contracts.Page[T], error)) ([]T, bool, error) {
	var items []T
	var cursor contracts.PageCursor
	for fetched := 1; fetched <= MaxPages; fetched++ {
		page, err := fetch(ctx, contracts.PageRequest{Cursor: cursor, Limit: min(contracts.MaxPageLimit, limit-len(items))})
		if err != nil {
			return nil, false, err
		}
		items = append(items, page.Items...)
		if len(items) >= limit {
			return items[:limit], page.Next != "" || len(items) > limit, nil
		}
		if page.Next == "" {
			return items, false, nil
		}
		cursor = page.Next
	}
	return items, true, nil
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
		HTTPURLToRepo     string `json:"http_url_to_repo"`
		Description       string `json:"description"`
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("glab repo list: parse: %w", err)
	}
	out := make([]Repo, 0, len(items))
	for _, it := range items {
		out = append(out, Repo{FullName: it.PathWithNamespace, CloneURL: gitlabCloneURL(it.SSHURLToRepo, it.HTTPURLToRepo), Desc: it.Description, Forge: gitlabLabel})
	}
	return out, nil
}

func gitlabCloneURL(sshURL, httpsURL string) string {
	if sshURL != "" {
		return sshURL
	}
	if strings.HasPrefix(httpsURL, "https://") {
		return httpsURL
	}
	return ""
}

// Clone clones spec into workspace and returns the project dir. spec is
// owner/name (github.com), or a clone URL over https or ssh. An existing target
// is returned as-is, before any credential is read. The GitHub token goes to git
// only for github.com over https and only when the host holds a valid one; any
// other clone runs anonymously.
func (c *Client) Clone(ctx context.Context, spec, workspace string) (string, error) {
	target, err := parseCloneTarget(spec)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(workspace, target.name)
	if _, statErr := os.Stat(dir); statErr == nil {
		return dir, nil
	}
	token, tokenErr := gitHubToken(ctx, c.creds, target)
	if tokenErr != nil {
		return "", fmt.Errorf("clone %s: %w", target.label, tokenErr)
	}
	env, envErr := gitEnvironment(os.Getenv, target.host, token)
	if envErr != nil {
		return "", fmt.Errorf("clone %s: %w", target.label, envErr)
	}
	out, err := c.r.run(ctx, workspace, env, "git", subClone, "--", target.cloneURL, dir)
	if err != nil {
		return "", fmt.Errorf("git clone: %s: %w", scrub(out, token), err)
	}
	return dir, nil
}

type cloneTarget struct {
	name        string
	label       string
	host        contracts.ForgeHost
	cloneURL    string
	githubHTTPS bool
}

var intranetHostRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*(:\d+)?$`)

func parseCloneTarget(spec string) (cloneTarget, error) {
	if specRe.MatchString(spec) {
		return ownerNameTarget(spec)
	}
	if !hasCloneScheme(spec) {
		return cloneTarget{}, invalidSpec(spec)
	}
	ref, err := contracts.ParseRepoRef(spec)
	if err != nil {
		return intranetTarget(spec)
	}
	https := strings.HasPrefix(spec, "https://")
	return cloneTarget{
		name:        ref.Name(),
		label:       ref.String(),
		host:        ref.Host(),
		cloneURL:    spec,
		githubHTTPS: https && ref.Host().String() == forgeport.GitHubHost,
	}, nil
}

func intranetTarget(spec string) (cloneTarget, error) {
	host, repoPath, ok := cloneLocation(spec)
	if !ok || !intranetHostRe.MatchString(host) || strings.ContainsAny(spec, "?#") {
		return cloneTarget{}, invalidSpec(spec)
	}
	name := path.Base(strings.TrimSuffix(repoPath, ".git"))
	if name == "." || name == ".." || name == "/" || strings.HasPrefix(name, "-") {
		return cloneTarget{}, invalidSpec(spec)
	}
	return cloneTarget{name: name, label: displaySpec(spec), cloneURL: spec}, nil
}

func ownerNameTarget(spec string) (cloneTarget, error) {
	ref, err := contracts.ParseRepoRef(forgeport.GitHubHost + "/" + spec)
	if err != nil {
		return cloneTarget{}, invalidSpec(spec)
	}
	return cloneTarget{
		name:        ref.Name(),
		label:       ref.String(),
		host:        ref.Host(),
		cloneURL:    "https://" + forgeport.GitHubHost + "/" + ref.Namespace() + "/" + ref.Name() + ".git",
		githubHTTPS: true,
	}, nil
}

func invalidSpec(spec string) error {
	return fmt.Errorf("%w: %q", ErrRepoSpec, displaySpec(spec))
}

func displaySpec(spec string) string {
	if parsed, err := url.Parse(spec); err == nil && parsed.Host != "" {
		parsed.User = nil
		parsed.RawQuery = ""
		parsed.Fragment = ""
		return parsed.String()
	}
	if strings.Contains(spec, "://") {
		if strings.ContainsAny(spec, "@?#") {
			return redactedMark
		}
		return spec
	}
	return displayScpSpec(spec)
}

func displayScpSpec(spec string) string {
	userEnd := strings.LastIndex(spec, "@")
	if userEnd < 0 {
		if strings.ContainsAny(spec, "?#") {
			return redactedMark
		}
		return spec
	}
	hostAndPath := spec[userEnd+1:]
	if strings.ContainsAny(hostAndPath, "?#") {
		return redactedMark
	}
	return redactedMark + "@" + hostAndPath
}

func hasCloneScheme(spec string) bool {
	return strings.HasPrefix(spec, "https://") || strings.HasPrefix(spec, "ssh://") || strings.HasPrefix(spec, "git@")
}

func cloneLocation(spec string) (string, string, bool) {
	switch {
	case strings.HasPrefix(spec, "git@"):
		host, repoPath, found := strings.Cut(strings.TrimPrefix(spec, "git@"), ":")
		return host, repoPath, found
	case strings.HasPrefix(spec, "ssh://"):
		return urlLocation(strings.TrimPrefix(spec, "ssh://"))
	case strings.HasPrefix(spec, "https://"):
		return urlLocation(strings.TrimPrefix(spec, "https://"))
	}
	return "", "", false
}

func urlLocation(rest string) (string, string, bool) {
	authority, repoPath, found := strings.Cut(rest, "/")
	if !found {
		return "", "", false
	}
	userinfo, host := "", authority
	if at := strings.LastIndex(authority, "@"); at >= 0 {
		userinfo, host = authority[:at], authority[at+1:]
	}
	return host, repoPath, !strings.HasPrefix(userinfo, "-")
}

func gitHubToken(ctx context.Context, creds contracts.ForgeCredentials, target cloneTarget) (contracts.ForgeToken, error) {
	if !target.githubHTTPS {
		return contracts.ForgeToken{}, nil
	}
	return hostToken(ctx, creds, target.host)
}

// GitEnvironment returns the environment that runs git against host. It stops git
// from prompting and, when creds holds a valid token for host, carries that token
// in GIT_CONFIG_* values only, never on a command line. Without a token the
// environment holds no credential.
func GitEnvironment(ctx context.Context, creds contracts.ForgeCredentials, host contracts.ForgeHost) ([]string, error) {
	token, err := hostToken(ctx, creds, host)
	if err != nil {
		return nil, err
	}
	return gitEnvironment(os.Getenv, host, token)
}

func hostToken(ctx context.Context, creds contracts.ForgeCredentials, host contracts.ForgeHost) (contracts.ForgeToken, error) {
	if creds == nil {
		creds = unconnected{}
	}
	token, err := creds.Token(ctx, host)
	if errors.Is(err, contracts.ErrForgeNotConnected) {
		return contracts.ForgeToken{}, nil
	}
	return token, err
}

func gitEnvironment(getenv func(string) string, host contracts.ForgeHost, token contracts.ForgeToken) ([]string, error) {
	env := []string{"GIT_TERMINAL_PROMPT=0"}
	if !token.Valid() {
		return env, nil
	}
	index, err := configIndex(getenv)
	if err != nil {
		return nil, err
	}
	key := strconv.Itoa(index)
	return append(env,
		configCount+"="+strconv.Itoa(index+1),
		"GIT_CONFIG_KEY_"+key+"=http.https://"+host.String()+"/.extraheader",
		"GIT_CONFIG_VALUE_"+key+"=Authorization: Basic "+basicAuth(token.Secret()),
	), nil
}

func configIndex(getenv func(string) string) (int, error) {
	raw := getenv(configCount)
	if raw == "" {
		return 0, nil
	}
	count, err := strconv.Atoi(raw)
	if err != nil || count < 0 || count > maxConfigs {
		return 0, ErrConfigCount
	}
	return count, nil
}

func basicAuth(secret string) string {
	return base64.StdEncoding.EncodeToString([]byte(tokenUser + ":" + secret))
}

func scrub(out []byte, token contracts.ForgeToken) string {
	if !token.Valid() {
		return redact.Output(out)
	}
	secret := token.Secret()
	masked := strings.NewReplacer(secret, redactedMark, basicAuth(secret), redactedMark).Replace(string(out))
	return redact.Output([]byte(masked))
}
