package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Herrscherd/herrscher/core/config"
	"github.com/Herrscherd/herrscher/core/host"
)

func TestConfigJSONCompactionReachesTheHostOptions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"compactAt": 60, "compactStack": ["backend"], "contextLimits": {"codex": 400000}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	var opts host.Options
	withCompactionConfig(&opts, cfg)
	if opts.CompactPercent == nil || *opts.CompactPercent != 60 {
		t.Fatalf("CompactPercent = %v, want 60 from compactAt", opts.CompactPercent)
	}
	if len(opts.CompactStack) != 1 || opts.CompactStack[0] != "backend" {
		t.Fatalf("CompactStack = %v, want [backend]", opts.CompactStack)
	}
	if opts.ContextLimits["codex"] != 400000 {
		t.Fatalf("ContextLimits = %v, want codex 400000 from contextLimits", opts.ContextLimits)
	}
}
