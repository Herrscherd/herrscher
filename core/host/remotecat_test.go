package host

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestSupportedRemoteCategoriesAreExactlyMemory(t *testing.T) {
	if !SupportedRemoteCategory(contracts.CategoryMemory) {
		t.Fatal("memory must be a supported remote category")
	}
	// Spec C grows this set later (orchestrator, backend); not yet.
	for _, c := range []contracts.Category{
		contracts.CategoryOrchestrator, contracts.CategoryBackend, contracts.CategoryGateway,
	} {
		if SupportedRemoteCategory(c) {
			t.Fatalf("%q must not be remote-supported yet (Spec C)", c)
		}
	}
	if n := len(supportedRemoteCategories); n != 1 {
		t.Fatalf("expected exactly one supported remote category (memory), got %d: %v", n, supportedRemoteCategories)
	}
}

func TestStartRemotePluginHostsSkipsUnsupportedWithWarning(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled: no real plugin-host process should be spawned

	// An unsupported remote category must be skipped with a logged warning
	// (Stage A logger), not hosted out-of-process.
	startRemotePluginHosts(ctx, "/bin/herrscher",
		map[contracts.Category]bool{contracts.CategoryBackend: true}, log)

	out := buf.String()
	if !strings.Contains(out, "not yet supported") || !strings.Contains(out, "category=backend") {
		t.Fatalf("expected an unsupported-category warning for backend, got: %q", out)
	}
}
