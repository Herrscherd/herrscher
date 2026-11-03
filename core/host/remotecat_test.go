package host

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestSupportedRemoteCategoriesAreMemoryOrchestratorBackend(t *testing.T) {
	for _, c := range []contracts.Category{
		contracts.CategoryMemory, contracts.CategoryOrchestrator, contracts.CategoryBackend,
	} {
		if !SupportedRemoteCategory(c) {
			t.Fatalf("%q must be a supported remote category (C1/C2/C3)", c)
		}
	}
	if SupportedRemoteCategory(contracts.CategoryGateway) {
		t.Fatal("gateway must not be remote-supported")
	}
	if SupportedRemoteCategory(contracts.CategorySkills) {
		t.Fatal("skills must not be remote-supported")
	}
}

func TestStartRemotePluginHostsSkipsUnsupportedWithWarning(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	startRemotePluginHosts(ctx, "/bin/herrscher",
		categorySet(contracts.CategoryGateway), log)

	out := buf.String()
	if !strings.Contains(out, "not yet supported") || !strings.Contains(out, "category=gateway") {
		t.Fatalf("expected an unsupported-category warning for gateway, got: %q", out)
	}
}
