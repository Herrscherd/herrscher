package forgeport

import (
	"context"
	"errors"
	"os"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const testKind = "forgeport-test-fake"

type stubForge struct {
	contracts.Forge

	host contracts.ForgeHost
}

func TestMain(m *testing.M) {
	contracts.Register(contracts.Plugin{
		Manifest: contracts.Manifest{Kind: testKind, Category: contracts.CategoryForge},
		Forge: func(_ context.Context, _ contracts.PluginConfig, host contracts.ForgeHost, _ contracts.ForgeCredentials) (contracts.Forge, error) {
			return stubForge{host: host}, nil
		},
	})
	os.Exit(m.Run())
}

type noCreds struct{}

func (noCreds) Token(context.Context, contracts.ForgeHost) (contracts.ForgeToken, error) {
	return contracts.ForgeToken{}, contracts.ErrForgeNotConnected
}

func (noCreds) Invalidate(context.Context, contracts.ForgeHost) {}

func TestOpenUnregisteredKindMatchesErrNoPlugin(t *testing.T) {
	_, err := Open(context.Background(), "no-such-forge", contracts.ForgeHost{}, noCreds{})
	if !errors.Is(err, ErrNoPlugin) {
		t.Fatalf("err = %v, want ErrNoPlugin", err)
	}
}

func TestOpenBuildsTheRegisteredForgeForTheHost(t *testing.T) {
	host, err := contracts.ParseForgeHost("example.test")
	if err != nil {
		t.Fatal(err)
	}
	forge, err := Open(context.Background(), testKind, host, noCreds{})
	if err != nil {
		t.Fatal(err)
	}
	stub, ok := forge.(stubForge)
	if !ok {
		t.Fatalf("forge = %T, want stubForge", forge)
	}
	if stub.host != host {
		t.Fatalf("host = %v, want %v", stub.host, host)
	}
}
