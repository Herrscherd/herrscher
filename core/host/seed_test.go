package host

import (
	"context"
	"os"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestSelectBackendVendorPrecedence(t *testing.T) {
	plugins := []contracts.Plugin{
		{Manifest: contracts.Manifest{Kind: "claude"}, Backend: func(context.Context, contracts.PluginConfig) (contracts.Backend, error) { return seedBackend{}, nil }},
		{Manifest: contracts.Manifest{Kind: "codex"}, Backend: func(context.Context, contracts.PluginConfig) (contracts.Backend, error) { return seedBackend{}, nil }},
		{Manifest: contracts.Manifest{Kind: "cursor"}, Backend: func(context.Context, contracts.PluginConfig) (contracts.Backend, error) { return seedBackend{}, nil }},
	}
	tests := []struct {
		name    string
		session string
		env     string
		want    string
		wantErr string
	}{
		{name: "explicit beats env", session: "cursor", env: "codex", want: "cursor"},
		{name: "env beats fallback", env: "codex", want: "codex"},
		{name: "fallback is first", want: "claude"},
		{name: "unknown is an error", session: "gemini", env: "codex", wantErr: `unknown backend "gemini"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.env == "" {
				_ = os.Unsetenv("HERRSCHER_BACKEND")
			} else {
				t.Setenv("HERRSCHER_BACKEND", tt.env)
			}
			desired := tt.session
			if desired == "" {
				desired = os.Getenv("HERRSCHER_BACKEND")
			}
			plugin, err := selectBackend(desired, plugins)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("selectBackend error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if plugin.Manifest.Kind != tt.want {
				t.Fatalf("selected kind = %q, want %q", plugin.Manifest.Kind, tt.want)
			}
		})
	}
}

func TestBuildBackendSelectsByVendor(t *testing.T) {
	saved := contracts.Default
	t.Cleanup(func() { contracts.Default = saved })
	contracts.Default = contracts.Registry{}

	var built string
	makePlugin := func(kind string) contracts.Plugin {
		return contracts.Plugin{
			Manifest: contracts.Manifest{Kind: kind, Category: contracts.CategoryBackend},
			Backend: func(context.Context, contracts.PluginConfig) (contracts.Backend, error) {
				built = kind
				return seedBackend{}, nil
			},
		}
	}
	contracts.Default.Register(makePlugin("claude"))
	contracts.Default.Register(makePlugin("codex"))

	if _, err := BuildBackend(context.Background(), "codex", "codex --model gpt-5.6", "", "", ""); err != nil {
		t.Fatalf("BuildBackend: %v", err)
	}
	if built != "codex" {
		t.Fatalf("built %q, want codex", built)
	}
}

type seedSpyOrchestrator struct {
	consolidated bool
	closed       bool
}

func (s *seedSpyOrchestrator) Context(context.Context) string                          { return "" }
func (s *seedSpyOrchestrator) Observe(context.Context, contracts.Prompt, string) error { return nil }
func (s *seedSpyOrchestrator) Consolidate(context.Context) error {
	s.consolidated = true
	return nil
}
func (s *seedSpyOrchestrator) Close() error {
	s.closed = true
	return nil
}

func TestRunOneShotSeedWithConsolidatesAndCloses(t *testing.T) {
	old := oneShotBackendFactory
	t.Cleanup(func() { oneShotBackendFactory = old })
	oneShotBackendFactory = func(context.Context, state.Session) (contracts.Backend, error) {
		return seedBackend{}, nil
	}

	sess := state.Session{Name: "solo", ChannelID: "channel"}
	spy := &seedSpyOrchestrator{}
	if _, err := runOneShotSeedWith(context.Background(), sess, "tâche", spy); err != nil {
		t.Fatalf("runOneShotSeedWith: %v", err)
	}
	if !spy.consolidated {
		t.Fatal("expected consolidation after the one-shot turn")
	}
	if !spy.closed {
		t.Fatal("expected orchestrator close after the one-shot turn")
	}
}

type seedBackend struct{}

func (seedBackend) Respond(context.Context, contracts.Prompt, func(contracts.BackendEvent)) (string, error) {
	return "reply", nil
}
func (seedBackend) Close() error { return nil }
