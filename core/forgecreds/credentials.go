package forgecreds

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const (
	envGHToken    = "GH_TOKEN"
	envGitHubAuth = "GITHUB_TOKEN"
	envGHHost     = "GH_HOST"
	gitHubDotCom  = "github.com"
	flowTimeout   = 30 * time.Second
)

// Credentials implements contracts.ForgeCredentials. A token is looked up in
// this order, and the first valid one wins: the token saved by herrscher
// connect for the host, then GH_TOKEN and GITHUB_TOKEN for github.com or for
// the host named by GH_HOST, then the login of gh auth token. A token that is
// not saved is kept in memory until Invalidate drops it.
type Credentials struct {
	store  *Store
	flow   Flow
	base   func(contracts.ForgeHost) string
	env    func(string) (string, bool)
	gitHub func(context.Context, string) (string, error)
	now    func() time.Time
	mu     sync.Mutex
	cache  map[string]contracts.ForgeToken
}

// New builds the credentials kept in the host state directory of stateFile.
func New(stateFile string) *Credentials {
	return &Credentials{
		store: NewStore(filepath.Dir(stateFile)),
		flow: Flow{
			http:     &http.Client{Timeout: flowTimeout},
			clientID: ClientID,
			wait:     waitFor,
			now:      time.Now,
		},
		base:   defaultBase,
		env:    os.LookupEnv,
		gitHub: gitHubCLIToken,
		now:    time.Now,
		cache:  make(map[string]contracts.ForgeToken),
	}
}

// Store returns the file the saved records of the credentials live in.
func (c *Credentials) Store() *Store {
	return c.store
}

func defaultBase(host contracts.ForgeHost) string {
	return "https://" + host.String()
}

// Token returns the credential of host, from the first source that holds a
// valid one. It returns [contracts.ErrForgeNotConnected] when none does.
func (c *Credentials) Token(ctx context.Context, host contracts.ForgeHost) (contracts.ForgeToken, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	saved, found, err := c.savedToken(ctx, host)
	if err != nil {
		return contracts.ForgeToken{}, err
	}
	if found {
		return saved, nil
	}
	if token, remembered := c.cache[host.String()]; remembered {
		return token, nil
	}
	token, err := c.fallbackToken(ctx, host)
	if err != nil {
		return contracts.ForgeToken{}, fmt.Errorf("%s: %w", host, err)
	}
	c.cache[host.String()] = token
	return token, nil
}

// Invalidate forgets the token remembered for host, and marks the saved token
// of host as refused so that status reports it. The saved token is kept, not
// erased: connect again to replace it.
func (c *Credentials) Invalidate(_ context.Context, host contracts.ForgeHost) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.cache, host.String())
	if err := c.store.Reject(host); err != nil {
		slog.Warn("mark forge token refused", "host", host.String(), "err", err)
	}
}

// Connect runs the device flow of the GitHub App against host, passes the
// code to show so the user can enter it, and saves the tokens granted.
func (c *Credentials) Connect(ctx context.Context, host contracts.ForgeHost, show func(DeviceCode) error) error {
	base := c.base(host)
	code, err := c.flow.Begin(ctx, base)
	if err != nil {
		return err
	}
	if showErr := show(code); showErr != nil {
		return showErr
	}
	record, err := c.flow.Await(ctx, base, code)
	if err != nil {
		return err
	}
	return c.store.Put(host, record)
}

func (c *Credentials) savedToken(ctx context.Context, host contracts.ForgeHost) (contracts.ForgeToken, bool, error) {
	record, found, err := c.store.Get(host)
	if err != nil || !found || record.Rejected {
		return contracts.ForgeToken{}, false, err
	}
	now := c.now()
	if needsRefresh(record, now) {
		refreshed, refreshErr := c.flow.Refresh(ctx, c.base(host), record)
		switch {
		case refreshErr == nil:
			record = refreshed
			if putErr := c.store.Put(host, record); putErr != nil {
				return contracts.ForgeToken{}, false, putErr
			}
		case errors.Is(refreshErr, ErrRefreshRejected):
			if markErr := c.store.Reject(host); markErr != nil {
				return contracts.ForgeToken{}, false, markErr
			}
			return contracts.ForgeToken{}, false, nil
		case stillValid(record, now):
		default:
			return contracts.ForgeToken{}, false, fmt.Errorf("%s: refresh the saved token: %w", host, refreshErr)
		}
	}
	token, err := contracts.NewForgeToken(record.AccessToken, record.Login, contracts.TokenSourceSaved)
	if err != nil {
		return contracts.ForgeToken{}, false, fmt.Errorf("%s: saved token: %w", host, err)
	}
	return token, true, nil
}

func (c *Credentials) fallbackToken(ctx context.Context, host contracts.ForgeHost) (contracts.ForgeToken, error) {
	if secret, ok := c.environmentSecret(host); ok {
		return contracts.NewForgeToken(secret, "", contracts.TokenSourceEnv)
	}
	secret, err := c.gitHub(ctx, host.String())
	if err != nil || secret == "" {
		return contracts.ForgeToken{}, contracts.ErrForgeNotConnected
	}
	return contracts.NewForgeToken(secret, "", contracts.TokenSourceCLI)
}

func (c *Credentials) environmentSecret(host contracts.ForgeHost) (string, bool) {
	if !c.environmentApplies(host) {
		return "", false
	}
	for _, name := range []string{envGHToken, envGitHubAuth} {
		if value, ok := c.env(name); ok {
			if secret := strings.TrimSpace(value); secret != "" {
				return secret, true
			}
		}
	}
	return "", false
}

func (c *Credentials) environmentApplies(host contracts.ForgeHost) bool {
	if host.String() == gitHubDotCom {
		return true
	}
	named, ok := c.env(envGHHost)
	if !ok {
		return false
	}
	parsed, err := contracts.ParseForgeHost(named)
	return err == nil && parsed == host
}

func needsRefresh(record Record, now time.Time) bool {
	if record.RefreshToken == "" || record.AccessExpiresAt.IsZero() {
		return false
	}
	return !now.Add(refreshSkew).Before(record.AccessExpiresAt)
}

func stillValid(record Record, now time.Time) bool {
	return record.AccessExpiresAt.IsZero() || now.Before(record.AccessExpiresAt)
}
