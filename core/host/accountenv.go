package host

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/accounts"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/keyproxy"
)

const (
	codexProxyHomeDir    = "herrscher-proxy"
	codexProxyConfigFile = "config.toml"
	codexProxyGrantEnv   = "HERRSCHER_KEY_PROXY_GRANT"
	proxyDirMode         = 0o700
	proxyFileMode        = 0o600
)

func providerByVendor(vendor string) (string, bool) {
	switch vendor {
	case contracts.VendorClaude:
		return "anthropic", true
	case contracts.VendorCodex:
		return "openai", true
	}
	return "", false
}

func accountRoot(req BackendRequest) (string, error) {
	if req.AccountRoot != "" {
		return req.AccountRoot, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", wrapEnglish(err, "account.home", nil)
	}
	return accounts.DefaultRoot(home), nil
}

func accountStore(req BackendRequest) (accounts.Store, error) {
	root, err := accountRoot(req)
	if err != nil {
		return accounts.Store{}, err
	}
	return accounts.Store{Root: root}, nil
}

func sessionEnv(req BackendRequest, vendor string, route modelRoute) (map[string]string, func(), error) {
	if len(route.env) > 0 {
		return route.env, route.lease(), nil
	}
	env := make(map[string]string)
	store, err := accountStore(req)
	if err != nil {
		return nil, noRelease, err
	}
	_, profileDir, err := store.Resolve(req.Account, vendor)
	if err != nil {
		return nil, noRelease, err
	}
	if profileDir != "" {
		profileVars, profileErr := contracts.ProfileEnv(vendor, profileDir)
		if profileErr != nil {
			return nil, noRelease, profileErr
		}
		mergeInto(env, profileVars)
	}
	provider, known := providerByVendor(vendor)
	if req.Keys == nil || !known {
		return env, noRelease, nil
	}
	upstream, configured := req.Keys.Provider(provider)
	if !configured {
		return env, noRelease, nil
	}
	grant, err := req.Keys.Grant(upstream)
	if err != nil {
		return nil, noRelease, err
	}
	if vendor == contracts.VendorCodex && profileDir != "" {
		proxyEnv, proxyErr := codexProfileProxyEnv(profileDir, grant)
		if proxyErr != nil {
			grant.Release()
			return nil, noRelease, proxyErr
		}
		return proxyEnv, grant.Release, nil
	}
	mergeInto(env, proxyEnvFor(vendor, grant))
	return env, grant.Release, nil
}

func noRelease() {}

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

func codexProfileProxyEnv(profileDir string, grant keyproxy.Grant) (map[string]string, error) {
	root, err := os.OpenRoot(profileDir)
	if err != nil {
		return nil, wrapEnglish(err, "account.profile_open", nil)
	}
	writeErr := errors.Join(writeCodexProxyHome(root, codexProxyConfig(grant.BaseURL+"/v1")), root.Close())
	if writeErr != nil {
		return nil, wrapEnglish(writeErr, "account.codex_home", nil)
	}
	return map[string]string{
		contracts.EnvCodexHome: filepath.Join(profileDir, codexProxyHomeDir),
		codexProxyGrantEnv:     grant.Token,
	}, nil
}

func writeCodexProxyHome(root *os.Root, config string) error {
	if mkErr := root.Mkdir(codexProxyHomeDir, proxyDirMode); mkErr != nil && !errors.Is(mkErr, fs.ErrExist) {
		return wrapEnglish(mkErr, "account.codex_home_create", nil)
	}
	file, err := root.OpenFile(filepath.Join(codexProxyHomeDir, codexProxyConfigFile), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, proxyFileMode)
	if err != nil {
		return wrapEnglish(err, "account.codex_config_open", nil)
	}
	_, writeErr := file.WriteString(config)
	if joined := errors.Join(writeErr, file.Close()); joined != nil {
		return wrapEnglish(joined, "account.codex_config_write", nil)
	}
	return nil
}

func codexProxyConfig(baseURL string) string {
	return fmt.Sprintf("model_provider = \"herrscher\"\n\n[model_providers.herrscher]\nname = \"Herrscher key proxy\"\nbase_url = %q\nenv_key = %q\nwire_api = \"responses\"\n", baseURL, codexProxyGrantEnv)
}

func mergeInto(dst, src map[string]string) {
	for key, value := range src {
		dst[key] = value
	}
}

func persistedSessions() ([]state.Session, error) {
	persisted, err := state.LoadState(DefaultStatePath())
	if err != nil {
		return nil, wrapEnglish(err, "session.read", nil)
	}
	return persisted.SnapshotSessions(), nil
}

// ProfileUsers lists the persisted sessions using a profile, following the vendor default when isDefault.
func ProfileUsers(vendor, name string, isDefault bool) ([]string, error) {
	sessions, err := persistedSessions()
	if err != nil {
		return nil, err
	}
	return profileUsers(sessions, vendor, name, isDefault), nil
}

func profileUsers(sessions []state.Session, vendor, name string, isDefault bool) []string {
	var names []string
	for index := range sessions {
		session := &sessions[index]
		if session.Vendor != vendor {
			continue
		}
		if session.Account == name || (isDefault && session.Account == "") {
			names = append(names, session.Name)
		}
	}
	return names
}

// ResumingSessions lists the sessions of vendor resuming under the vendor default.
func ResumingSessions(vendor string) ([]string, error) {
	sessions, err := persistedSessions()
	if err != nil {
		return nil, err
	}
	return resumingUnder(sessions, vendor), nil
}

func resumingUnder(sessions []state.Session, vendor string) []string {
	var names []string
	for index := range sessions {
		if sessions[index].Vendor == vendor && sessions[index].Account == "" && sessions[index].ResumeToken != "" {
			names = append(names, sessions[index].Name)
		}
	}
	return names
}

const neubloxKeyName = "neublox"

func spawnEnvVia(entry CatalogEntry, getenv func(string) string, keys *keyproxy.Proxy) (map[string]string, func(), error) {
	if entry.Route != contracts.RouteGateway || keys == nil {
		env, err := spawnEnvFor(entry, getenv)
		return env, noRelease, err
	}
	creds, err := loadGatewayCreds(getenv)
	if err != nil {
		return nil, noRelease, wrapEnglish(err, "gateway.model", map[string]any{"Model": entry.ID})
	}
	env := gatewayEnvFor(entry.Vendor, creds, entry.Arg)
	if len(env) == 0 {
		return nil, noRelease, &modelNotRedirectableError{Model: entry.ID, Backend: entry.Vendor}
	}
	upstream, err := url.Parse(creds.BaseURL())
	if err != nil {
		return nil, noRelease, wrapEnglish(err, "gateway.base_url", nil)
	}
	route := keyproxy.GatewayRoute(upstream, gatewayAuthStyle(entry.Vendor), keyproxy.NewSecret(neubloxKeyName, creds.Token()))
	grant, err := keys.Grant(route)
	if err != nil {
		return nil, noRelease, wrapEnglish(err, "gateway.key_proxy", nil)
	}
	return gatewayEnvThroughProxy(entry.Vendor, env, grant), grant.Release, nil
}

func gatewayAuthStyle(vendor string) keyproxy.AuthStyle {
	if vendor == backendCodex {
		return keyproxy.AuthBearer
	}
	return keyproxy.AuthXAPIKey
}

func gatewayEnvThroughProxy(vendor string, env map[string]string, grant keyproxy.Grant) map[string]string {
	baseKey, tokenKey := contracts.EnvAnthropicBaseURL, contracts.EnvAnthropicAPIKey
	if vendor == backendCodex {
		baseKey, tokenKey = contracts.EnvOpenAIBaseURL, contracts.EnvNeubloxToken
	}
	env[baseKey] = grant.BaseURL
	env[tokenKey] = grant.Token
	return env
}
