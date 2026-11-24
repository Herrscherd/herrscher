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
	appRetryDelay = time.Minute
)

// Credentials implements contracts.ForgeCredentials. A token is looked up in
// this order, and the first valid one wins: the token saved by herrscher
// connect for the host, then GH_TOKEN and GITHUB_TOKEN for github.com or for
// the host named by GH_HOST, then the login of gh auth token. A token that is
// not saved is kept in memory until Invalidate drops it.
//
// The saved token of a host is renewed under a lock of that host, shared by
// the processes of the state directory, so they never spend the same refresh
// token twice. A slow renewal holds up that host only. A renewal the file could
// not record is kept in memory for this process and recorded again on the next
// call. When the forge does not accept the herrscher app, a saved token that is
// still valid is served with a warning, and Token returns ErrAppMisconfigured
// only once it has expired; AppMisconfigured reports the state. Nothing is
// marked as refused.
type Credentials struct {
	store       *Store
	flow        Flow
	base        func(contracts.ForgeHost) string
	env         func(string) (string, bool)
	gitHub      func(context.Context, contracts.ForgeHost) (string, error)
	now         func() time.Time
	mu          sync.Mutex
	guards      map[string]*sync.Mutex
	cache       map[string]contracts.ForgeToken
	served      map[string]string
	held        map[string]heldRecord
	appBadUntil map[string]time.Time
}

type heldRecord struct {
	record           Record
	from             string
	warnedUnreadable bool
	warnedUnsaved    bool
}

type settlement struct {
	record Record
	ok     bool
	write  bool
	from   string
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
		base:        defaultBase,
		env:         os.LookupEnv,
		gitHub:      gitHubCLIToken,
		now:         time.Now,
		guards:      make(map[string]*sync.Mutex),
		cache:       make(map[string]contracts.ForgeToken),
		served:      make(map[string]string),
		held:        make(map[string]heldRecord),
		appBadUntil: make(map[string]time.Time),
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
	saved, found, err := c.savedToken(ctx, host)
	if err != nil {
		return contracts.ForgeToken{}, err
	}
	if found {
		return saved, nil
	}
	key := host.String()
	c.mu.Lock()
	remembered, ok := c.cache[key]
	c.mu.Unlock()
	if ok {
		return remembered, nil
	}
	token, err := c.fallbackToken(ctx, host)
	if err != nil {
		return contracts.ForgeToken{}, fmt.Errorf("%s: %w", host, err)
	}
	c.mu.Lock()
	c.cache[key] = token
	c.mu.Unlock()
	return token, nil
}

// Invalidate forgets the token remembered for host. When the forge refused the
// saved access token this process last served for host, it marks that saved
// token as refused so that status reports it. The saved token is kept, not
// erased: connect again to replace it. The contract passes no token, so a
// refusal of a token that was replaced by a renewal after it was served is
// not recognized as such; it is marked only while it is still the last one
// served.
func (c *Credentials) Invalidate(ctx context.Context, host contracts.ForgeHost) {
	key := host.String()
	c.mu.Lock()
	delete(c.cache, key)
	served, wasServed := c.served[key]
	delete(c.served, key)
	c.mu.Unlock()
	if !wasServed {
		return
	}
	guard := c.guard(host)
	guard.Lock()
	defer guard.Unlock()
	marked := false
	err := c.store.Update(ctx, func(transaction *Tx) error {
		current, found := transaction.Get(host)
		if !found {
			return nil
		}
		if pending, ok := c.heldFor(host); ok && current.RefreshToken == pending.from {
			current = pending.record
		}
		if current.AccessToken != served {
			return nil
		}
		current.Rejected = true
		transaction.Put(host, current)
		marked = true
		return nil
	})
	if err != nil {
		slog.Warn("mark forge token refused", "host", key, "err", err)
		return
	}
	if marked {
		c.dropHeld(host)
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
	guard := c.guard(host)
	guard.Lock()
	defer guard.Unlock()
	if err := c.store.Put(ctx, host, record); err != nil {
		return err
	}
	c.dropHeld(host)
	return nil
}

func (c *Credentials) savedToken(ctx context.Context, host contracts.ForgeHost) (contracts.ForgeToken, bool, error) {
	guard := c.guard(host)
	guard.Lock()
	defer guard.Unlock()
	record, usable, err := c.savedRecord(ctx, host)
	if err != nil || !usable {
		return contracts.ForgeToken{}, false, err
	}
	token, err := contracts.NewForgeToken(record.AccessToken, record.Login, contracts.TokenSourceSaved)
	if err != nil {
		return contracts.ForgeToken{}, false, fmt.Errorf("%s: saved token: %w", host, err)
	}
	c.mu.Lock()
	c.served[host.String()] = record.AccessToken
	c.mu.Unlock()
	return token, true, nil
}

// AppMisconfigured reports whether the last renewal of host was refused because
// the forge does not accept the herrscher app credentials. It is false until a
// renewal of host runs again in this process.
func (c *Credentials) AppMisconfigured(host contracts.ForgeHost) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, bad := c.appBadUntil[host.String()]
	return bad
}

func (c *Credentials) setAppMisconfigured(host contracts.ForgeHost, retryAt time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.appBadUntil[host.String()] = retryAt
}

func (c *Credentials) clearAppMisconfigured(host contracts.ForgeHost) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.appBadUntil, host.String())
}

func (c *Credentials) appRetryHeld(host contracts.ForgeHost, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	retryAt, bad := c.appBadUntil[host.String()]
	return bad && now.Before(retryAt)
}

func (c *Credentials) savedRecord(ctx context.Context, host contracts.ForgeHost) (Record, bool, error) {
	record, found, err := c.store.Get(host)
	if err != nil {
		return c.heldWhileUnreadable(ctx, host, err)
	}
	c.markReadable(host)
	now := c.now()
	_, pending := c.heldFor(host)
	if !found {
		c.dropHeld(host)
		return Record{}, false, nil
	}
	if !pending && !needsRefresh(record, now) {
		return record, !record.Rejected && !record.Expired(now), nil
	}
	return c.settle(ctx, host)
}

func (c *Credentials) settle(ctx context.Context, host contracts.ForgeHost) (Record, bool, error) {
	release, err := c.store.lockRenewal(ctx, host)
	if err != nil {
		return c.settleFailed(host, settlement{}, err)
	}
	defer release()
	outcome, err := c.renew(ctx, host)
	if err != nil {
		return c.settleFailed(host, outcome, err)
	}
	if outcome.write {
		outcome, err = c.persist(ctx, host, outcome)
		if err != nil {
			return c.settleFailed(host, outcome, err)
		}
	}
	c.dropHeld(host)
	return outcome.record, outcome.ok, nil
}

func (c *Credentials) renew(ctx context.Context, host contracts.ForgeHost) (settlement, error) {
	now := c.now()
	current, found, err := c.store.Get(host)
	if err != nil {
		return settlement{}, err
	}
	if !found || current.Rejected {
		c.dropHeld(host)
		return settlement{}, nil
	}
	pending, pendingFound := c.heldFor(host)
	if pendingFound && current.RefreshToken != pending.from {
		c.dropHeld(host)
		pendingFound = false
	}
	if pendingFound {
		return settlement{record: pending.record, ok: true, write: true, from: pending.from}, nil
	}
	if !needsRefresh(current, now) {
		return settlement{record: current, ok: !current.Expired(now)}, nil
	}
	if current.Expired(now) {
		return settlement{}, nil
	}
	refreshed, deferred, err := c.refresh(ctx, host, current)
	switch {
	case errors.Is(err, ErrRefreshRejected):
		current.Rejected = true
		return settlement{record: current, write: true, from: current.RefreshToken}, nil
	case err != nil:
		if !stillValid(current, now) {
			return settlement{}, fmt.Errorf("%s: refresh the saved token: %w", host, err)
		}
		if !deferred {
			slog.Warn("refresh the saved forge token", "host", host.String(), "err", err)
		}
		return settlement{record: current, ok: true}, nil
	}
	return settlement{record: refreshed, ok: true, write: true, from: current.RefreshToken}, nil
}

func (c *Credentials) persist(ctx context.Context, host contracts.ForgeHost, outcome settlement) (settlement, error) {
	now := c.now()
	saved := outcome
	err := c.store.Update(ctx, func(transaction *Tx) error {
		current, found := transaction.Get(host)
		switch {
		case !found:
			saved = settlement{}
		case current.RefreshToken != outcome.from:
			saved = settlement{record: current, ok: !current.Rejected && !current.Expired(now)}
		default:
			transaction.Put(host, outcome.record)
			saved = settlement{record: outcome.record, ok: outcome.ok}
		}
		return nil
	})
	if err != nil {
		return outcome, err
	}
	return saved, nil
}

func (c *Credentials) settleFailed(host contracts.ForgeHost, outcome settlement, err error) (Record, bool, error) {
	if outcome.ok && outcome.write {
		c.holdUnsaved(host, heldRecord{record: outcome.record, from: outcome.from}, err)
		return outcome.record, true, nil
	}
	if pending, ok := c.heldFor(host); ok && stillValid(pending.record, c.now()) {
		slog.Warn("forge renewal not finished, using the renewed token kept in memory", "host", host.String(), "err", err)
		return pending.record, true, nil
	}
	return Record{}, false, err
}

func (c *Credentials) heldWhileUnreadable(ctx context.Context, host contracts.ForgeHost, readErr error) (Record, bool, error) {
	pending, ok := c.heldFor(host)
	if !ok || !stillValid(pending.record, c.now()) {
		return Record{}, false, readErr
	}
	c.noteUnreadable(host, readErr)
	if err := c.retryHeld(ctx, host, pending); err != nil {
		slog.Debug("forge connections not saved yet, the renewed token is kept in memory", "host", host.String(), "err", err)
	}
	return pending.record, true, nil
}

func (c *Credentials) retryHeld(ctx context.Context, host contracts.ForgeHost, pending heldRecord) error {
	release, err := c.store.lockRenewal(ctx, host)
	if err != nil {
		return err
	}
	defer release()
	if _, err := c.persist(ctx, host, settlement{record: pending.record, ok: true, write: true, from: pending.from}); err != nil {
		return err
	}
	c.dropHeld(host)
	return nil
}

func (c *Credentials) holdUnsaved(host contracts.ForgeHost, held heldRecord, saveErr error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := host.String()
	if previous, ok := c.held[key]; ok {
		held.warnedUnreadable = previous.warnedUnreadable
		held.warnedUnsaved = previous.warnedUnsaved
	}
	if !held.warnedUnsaved {
		held.warnedUnsaved = true
		slog.Warn("forge connections not saved, the renewed token is kept in memory", "host", key, "err", saveErr)
	}
	c.held[key] = held
}

func (c *Credentials) noteUnreadable(host contracts.ForgeHost, readErr error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	held, ok := c.held[host.String()]
	if !ok || held.warnedUnreadable {
		return
	}
	held.warnedUnreadable = true
	c.held[host.String()] = held
	slog.Warn("forge connections not readable, using the renewed token kept in memory", "host", host.String(), "err", readErr)
}

func (c *Credentials) markReadable(host contracts.ForgeHost) {
	c.mu.Lock()
	defer c.mu.Unlock()
	held, ok := c.held[host.String()]
	if !ok {
		return
	}
	held.warnedUnreadable = false
	c.held[host.String()] = held
}

func (c *Credentials) refresh(ctx context.Context, host contracts.ForgeHost, current Record) (Record, bool, error) {
	now := c.now()
	if c.appRetryHeld(host, now) {
		return Record{}, true, ErrAppMisconfigured
	}
	refreshed, err := c.flow.Refresh(ctx, c.base(host), current)
	c.noteAppStanding(host, err, now)
	return refreshed, false, err
}

func (c *Credentials) noteAppStanding(host contracts.ForgeHost, refreshErr error, now time.Time) {
	switch {
	case errors.Is(refreshErr, ErrAppMisconfigured):
		c.setAppMisconfigured(host, now.Add(appRetryDelay))
	case refreshErr == nil, errors.Is(refreshErr, ErrRefreshRejected):
		c.clearAppMisconfigured(host)
	}
}

func (c *Credentials) fallbackToken(ctx context.Context, host contracts.ForgeHost) (contracts.ForgeToken, error) {
	if secret, ok := c.environmentSecret(host); ok {
		return contracts.NewForgeToken(secret, "", contracts.TokenSourceEnv)
	}
	secret, err := c.gitHub(ctx, host)
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

func (c *Credentials) guard(host contracts.ForgeHost) *sync.Mutex {
	c.mu.Lock()
	defer c.mu.Unlock()
	guard, ok := c.guards[host.String()]
	if !ok {
		guard = &sync.Mutex{}
		c.guards[host.String()] = guard
	}
	return guard
}

func (c *Credentials) heldFor(host contracts.ForgeHost) (heldRecord, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	held, ok := c.held[host.String()]
	return held, ok
}

func (c *Credentials) dropHeld(host contracts.ForgeHost) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.held, host.String())
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
