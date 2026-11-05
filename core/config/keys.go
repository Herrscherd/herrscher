package config

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
)

const (
	// StrategyFailover moves to the next key only when the current one fails.
	StrategyFailover = "failover"
	// StrategyRoundRobin rotates through the keys in turn.
	StrategyRoundRobin = "round_robin"
)

const (
	// ProviderAnthropic names the Anthropic API key pool.
	ProviderAnthropic = "anthropic"
	// ProviderOpenAI names the OpenAI API key pool.
	ProviderOpenAI = "openai"
)

const (
	defaultCooldownSeconds = 300
	// DefaultMaxBodyBytes is the request body cap the proxy buffers for replay.
	DefaultMaxBodyBytes = 32 << 20
)

func defaultFailoverStatuses() []int {
	return []int{401, 429, 529}
}

var (
	// ErrKeysProvider reports a provider name the proxy does not serve.
	ErrKeysProvider = errors.New("unknown key provider")
	// ErrKeysStrategy reports a strategy other than failover or round_robin.
	ErrKeysStrategy = errors.New("unknown key strategy")
	// ErrKeysStatus reports a failover status outside 400 to 599.
	ErrKeysStatus = errors.New("failover status outside 400 to 599")
	// ErrKeysSource reports a key source with no origin, or with more than one.
	ErrKeysSource = errors.New("a key needs exactly one of key_env, key_file or key_cmd")
	// ErrKeysSourceName reports a key name that is empty or listed twice in one provider.
	ErrKeysSourceName = errors.New("a key name is empty or listed twice")
	// ErrKeysCommand reports a key_cmd with no program to run.
	ErrKeysCommand = errors.New("key_cmd names no program")
	// ErrKeysBaseURL reports a base URL that is not an absolute http or https URL.
	ErrKeysBaseURL = errors.New("key base URL is not an absolute http or https URL")
	// ErrKeysCooldown reports a negative cooldown or body cap.
	ErrKeysCooldown = errors.New("cooldownSeconds and maxBodyBytes must not be negative")
	// ErrKeysNoSource reports a provider with no key to send requests with.
	ErrKeysNoSource = errors.New("a provider needs at least one key source")
)

// KeysConfig is the keys section of config.json: the key pools the local proxy rotates across.
type KeysConfig struct {
	Proxy     *bool                   `json:"proxy,omitempty"`
	Providers map[string]ProviderKeys `json:"providers,omitempty"`
}

// ProviderKeys is one provider pool of keys and the rules that rotate them.
type ProviderKeys struct {
	BaseURL         string      `json:"baseURL,omitempty"`
	Strategy        string      `json:"strategy,omitempty"`
	Statuses        []int       `json:"statuses,omitempty"`
	CooldownSeconds int         `json:"cooldownSeconds,omitempty"`
	MaxBodyBytes    int64       `json:"maxBodyBytes,omitempty"`
	Sources         []KeySource `json:"sources"`
}

// KeySource says where one key is read from: exactly one of its three origins is set.
type KeySource struct {
	Name    string   `json:"name"`
	KeyEnv  string   `json:"key_env,omitempty"`
	KeyFile string   `json:"key_file,omitempty"`
	KeyCmd  []string `json:"key_cmd,omitempty"`
}

// ProxyEnabled reports whether the local proxy runs.
func (k *KeysConfig) ProxyEnabled() bool {
	if k == nil || len(k.Providers) == 0 {
		return false
	}
	return k.Proxy == nil || *k.Proxy
}

// ValidateKeys checks every provider and key source, wrapping the first failing sentinel.
func ValidateKeys(keys *KeysConfig) error {
	if keys == nil {
		return nil
	}
	for _, provider := range sortedProviders(keys.Providers) {
		if err := validateProvider(provider, keys.Providers[provider]); err != nil {
			return fmt.Errorf("keys.providers.%s: %w", provider, err)
		}
	}
	return nil
}

func sortedProviders(providers map[string]ProviderKeys) []string {
	names := make([]string, 0, len(providers))
	for name := range providers {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func validateProvider(provider string, pool ProviderKeys) error {
	if provider != ProviderAnthropic && provider != ProviderOpenAI {
		return fmt.Errorf("%q: %w", provider, ErrKeysProvider)
	}
	if err := validateBaseURL(pool.BaseURL); err != nil {
		return err
	}
	if err := validateStrategy(pool.Strategy); err != nil {
		return err
	}
	if err := validateStatuses(pool.Statuses); err != nil {
		return err
	}
	if pool.CooldownSeconds < 0 || pool.MaxBodyBytes < 0 {
		return ErrKeysCooldown
	}
	if len(pool.Sources) == 0 {
		return ErrKeysNoSource
	}
	return validateSources(pool.Sources)
}

func validateBaseURL(raw string) error {
	if raw == "" {
		return nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("%q: %w", raw, ErrKeysBaseURL)
	}
	return nil
}

func validateStrategy(strategy string) error {
	if strategy == "" || strategy == StrategyFailover || strategy == StrategyRoundRobin {
		return nil
	}
	return fmt.Errorf("%q: %w", strategy, ErrKeysStrategy)
}

func validateStatuses(statuses []int) error {
	for _, status := range statuses {
		if status < 400 || status > 599 {
			return fmt.Errorf("%d: %w", status, ErrKeysStatus)
		}
	}
	return nil
}

func validateSources(sources []KeySource) error {
	seen := make(map[string]bool, len(sources))
	for _, source := range sources {
		if strings.TrimSpace(source.Name) == "" || seen[source.Name] {
			return fmt.Errorf("%q: %w", source.Name, ErrKeysSourceName)
		}
		seen[source.Name] = true
		if err := validateSourceOrigin(source); err != nil {
			return fmt.Errorf("key %q: %w", source.Name, err)
		}
	}
	return nil
}

func validateSourceOrigin(source KeySource) error {
	origins := 0
	if source.KeyEnv != "" {
		origins++
	}
	if source.KeyFile != "" {
		origins++
	}
	if len(source.KeyCmd) > 0 {
		origins++
		if strings.TrimSpace(source.KeyCmd[0]) == "" {
			return ErrKeysCommand
		}
	}
	if origins != 1 {
		return ErrKeysSource
	}
	return nil
}

// EffectiveStrategy returns the strategy of a pool, with the default applied.
func (p ProviderKeys) EffectiveStrategy() string {
	if p.Strategy == "" {
		return StrategyFailover
	}
	return p.Strategy
}

// EffectiveStatuses returns the statuses that move a request to the next key.
func (p ProviderKeys) EffectiveStatuses() []int {
	if len(p.Statuses) == 0 {
		return defaultFailoverStatuses()
	}
	return slices.Clone(p.Statuses)
}

// EffectiveCooldownSeconds returns the cooldown without a Retry-After, with the default applied.
func (p ProviderKeys) EffectiveCooldownSeconds() int {
	if p.CooldownSeconds == 0 {
		return defaultCooldownSeconds
	}
	return p.CooldownSeconds
}

// EffectiveMaxBodyBytes returns the largest request body the proxy buffers, with the default applied.
func (p ProviderKeys) EffectiveMaxBodyBytes() int64 {
	if p.MaxBodyBytes == 0 {
		return DefaultMaxBodyBytes
	}
	return p.MaxBodyBytes
}
