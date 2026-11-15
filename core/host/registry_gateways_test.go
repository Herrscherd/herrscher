package host

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

type namedAdmin struct{ labeledAdmin }

func (a namedAdmin) CreateUnder(context.Context, string, string) (string, error) {
	return "", fmt.Errorf("admin %s was consulted", a.id)
}

func TestNewRegistryWiresTheTerminalAdmin(t *testing.T) {
	gws := []Deps{
		{Gateway: kindGateway{"chat"}, Admin: namedAdmin{labeledAdmin{"chat"}}},
		{Gateway: kindGateway{"terminal"}, Admin: namedAdmin{labeledAdmin{"terminal"}}},
	}
	reg, err := NewRegistry(context.Background(), gws, Options{Runtime: testRuntime(t), StatePath: t.TempDir() + "/s.json"})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	_, err = reg.Dispatch(context.Background(),
		[]string{"session", "create", "--name", "x", "--terminal_only", "--shared"})
	if err == nil {
		t.Fatal("create should have stopped at the channel admin")
	}
	if strings.Contains(err.Error(), "no home set") {
		t.Fatalf("terminal-only create asked for a home it does not need: %v", err)
	}
	if !strings.Contains(err.Error(), "admin terminal was consulted") {
		t.Fatalf("err = %v, want the terminal admin to have minted the channel", err)
	}
}

func TestNewRegistryWithoutTerminalGatewayStillNeedsAHome(t *testing.T) {
	reg, err := NewRegistry(context.Background(), nil, Options{Runtime: testRuntime(t), StatePath: t.TempDir() + "/s.json"})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	_, err = reg.Dispatch(context.Background(),
		[]string{"session", "create", "--name", "x", "--terminal_only", "--shared"})
	if err == nil || !strings.Contains(err.Error(), "no home set") {
		t.Fatalf("err = %v, want the no-home refusal", err)
	}
}
