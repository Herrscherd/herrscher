package config

import (
	"errors"
	"testing"
)

func TestValidateKeysAcceptsAWellFormedPool(t *testing.T) {
	keys := &KeysConfig{Providers: map[string]ProviderKeys{
		ProviderAnthropic: {Sources: []KeySource{{Name: "main", KeyEnv: "ANTHROPIC_KEY"}}},
		ProviderOpenAI: {
			Strategy: StrategyRoundRobin,
			Sources: []KeySource{
				{Name: "a", KeyFile: "~/keys/a"},
				{Name: "b", KeyCmd: []string{"pass", "show", "openai"}},
			},
		},
	}}
	if err := ValidateKeys(keys); err != nil {
		t.Fatalf("ValidateKeys: %v", err)
	}
}

func TestValidateKeysRejections(t *testing.T) {
	cases := map[string]struct {
		keys *KeysConfig
		want error
	}{
		"unknown provider": {
			keys: &KeysConfig{Providers: map[string]ProviderKeys{"cursor": {Sources: []KeySource{{Name: "k", KeyEnv: "X"}}}}},
			want: ErrKeysProvider,
		},
		"two origins on one source": {
			keys: &KeysConfig{Providers: map[string]ProviderKeys{ProviderAnthropic: {Sources: []KeySource{{Name: "k", KeyEnv: "X", KeyFile: "f"}}}}},
			want: ErrKeysSource,
		},
		"no origin on one source": {
			keys: &KeysConfig{Providers: map[string]ProviderKeys{ProviderAnthropic: {Sources: []KeySource{{Name: "k"}}}}},
			want: ErrKeysSource,
		},
		"empty key_cmd program": {
			keys: &KeysConfig{Providers: map[string]ProviderKeys{ProviderAnthropic: {Sources: []KeySource{{Name: "k", KeyCmd: []string{" "}}}}}},
			want: ErrKeysCommand,
		},
		"duplicate key name": {
			keys: &KeysConfig{Providers: map[string]ProviderKeys{ProviderAnthropic: {Sources: []KeySource{
				{Name: "k", KeyEnv: "A"}, {Name: "k", KeyEnv: "B"},
			}}}},
			want: ErrKeysSourceName,
		},
		"pool without any source": {
			keys: &KeysConfig{Providers: map[string]ProviderKeys{ProviderAnthropic: {}}},
			want: ErrKeysNoSource,
		},
		"base URL without scheme": {
			keys: &KeysConfig{Providers: map[string]ProviderKeys{ProviderAnthropic: {BaseURL: "api.example.com", Sources: []KeySource{{Name: "k", KeyEnv: "A"}}}}},
			want: ErrKeysBaseURL,
		},
		"unknown strategy": {
			keys: &KeysConfig{Providers: map[string]ProviderKeys{ProviderAnthropic: {Strategy: "random", Sources: []KeySource{{Name: "k", KeyEnv: "A"}}}}},
			want: ErrKeysStrategy,
		},
		"status outside error range": {
			keys: &KeysConfig{Providers: map[string]ProviderKeys{ProviderAnthropic: {Statuses: []int{200}, Sources: []KeySource{{Name: "k", KeyEnv: "A"}}}}},
			want: ErrKeysStatus,
		},
		"negative cooldown": {
			keys: &KeysConfig{Providers: map[string]ProviderKeys{ProviderAnthropic: {CooldownSeconds: -1, Sources: []KeySource{{Name: "k", KeyEnv: "A"}}}}},
			want: ErrKeysCooldown,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidateKeys(tc.keys); !errors.Is(err, tc.want) {
				t.Fatalf("ValidateKeys = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestProxyEnabledFollowsTheSwitch(t *testing.T) {
	off := false
	withPool := map[string]ProviderKeys{ProviderAnthropic: {Sources: []KeySource{{Name: "k", KeyEnv: "A"}}}}
	if (&KeysConfig{Providers: withPool}).ProxyEnabled() != true {
		t.Fatal("a configured pool should enable the proxy by default")
	}
	if (&KeysConfig{Providers: withPool, Proxy: &off}).ProxyEnabled() {
		t.Fatal("proxy: false should turn it off")
	}
	if (&KeysConfig{}).ProxyEnabled() {
		t.Fatal("an empty keys section should leave it off")
	}
}
