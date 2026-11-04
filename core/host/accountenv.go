package host

import (
	"errors"
	"fmt"
	"os"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/accounts"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/keyproxy"
)

// ErrAccountProxyCodex reports a codex profile combined with the key proxy, which the codex backend cannot carry.
var ErrAccountProxyCodex = errors.New("a codex account profile cannot run through the key proxy: the codex backend replaces CODEX_HOME on that route")

func providerByVendor(vendor string) (string, bool) {
	switch vendor {
	case contracts.VendorClaude:
		return "anthropic", true
	case contracts.VendorCodex:
		return "openai", true
	}
	return "", false
}

func accountStore(req BackendRequest) (accounts.Store, error) {
	if req.AccountRoot != "" {
		return accounts.Store{Root: req.AccountRoot}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return accounts.Store{}, fmt.Errorf("resolve home for account profiles: %w", err)
	}
	return accounts.Store{Root: accounts.DefaultRoot(home)}, nil
}

func sessionEnv(req BackendRequest, vendor string, route modelRoute) (map[string]string, error) {
	if len(route.env) > 0 {
		return route.env, nil
	}
	env := make(map[string]string)
	store, err := accountStore(req)
	if err != nil {
		return nil, err
	}
	_, profileDir, err := store.Resolve(req.Account, vendor)
	if err != nil {
		return nil, err
	}
	if profileDir != "" {
		profileVars, profileErr := contracts.ProfileEnv(vendor, profileDir)
		if profileErr != nil {
			return nil, profileErr
		}
		mergeInto(env, profileVars)
	}
	if req.Keys == nil {
		return env, nil
	}
	provider, known := providerByVendor(vendor)
	if !known {
		return env, nil
	}
	upstream, configured := req.Keys.Provider(provider)
	if !configured {
		return env, nil
	}
	if vendor == contracts.VendorCodex && profileDir != "" {
		return nil, ErrAccountProxyCodex
	}
	grant, err := req.Keys.Grant(upstream)
	if err != nil {
		return nil, err
	}
	mergeInto(env, proxyEnvFor(vendor, grant))
	return env, nil
}

func proxyEnvFor(vendor string, grant keyproxy.Grant) map[string]string {
	if vendor == contracts.VendorCodex {
		return map[string]string{
			contracts.EnvOpenAIBaseURL: grant.BaseURL + "/v1",
			contracts.EnvNeubloxToken:  grant.Token,
		}
	}
	return map[string]string{
		contracts.EnvAnthropicBaseURL: grant.BaseURL,
		contracts.EnvAnthropicAPIKey:  grant.Token,
	}
}

func mergeInto(dst, src map[string]string) {
	for key, value := range src {
		dst[key] = value
	}
}

// ProfileInUse reports whether a persisted session references the profile.
func ProfileInUse(vendor, name string) (bool, error) {
	persisted, err := state.LoadState(DefaultStatePath())
	if err != nil {
		return false, fmt.Errorf("read sessions: %w", err)
	}
	sessions := persisted.SnapshotSessions()
	for index := range sessions {
		if sessions[index].Vendor == vendor && sessions[index].Account == name {
			return true, nil
		}
	}
	return false, nil
}
