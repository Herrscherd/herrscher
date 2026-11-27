package main

import (
	"context"
	"errors"
	"testing"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/host"
)

func TestMCPWeightRoutesToTheWeightVerbNotTheSessionServer(t *testing.T) {
	t.Setenv(host.SessionCommandSocketVar, "")
	t.Setenv("HERRSCHER_STATE_DIR", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	err := runMCP(context.Background(), host.RuntimeLayout{}, clitext.NewMessages(), host.NewMessages(), []string{mcpWeightSubcommand})
	if errors.Is(err, errNoSessionSocket) {
		t.Fatalf("runMCP(weight) = %v, the weight verb fell back to the session MCP server", err)
	}
	if errors.Is(err, errUnknownArgument) {
		t.Fatalf("runMCP(weight) = %v, the weight verb was refused as an unknown argument", err)
	}
	t.Logf("runMCP(weight) = %v", err)
}
