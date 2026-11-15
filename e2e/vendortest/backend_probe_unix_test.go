//go:build unix

package vendortest

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"

	_ "github.com/Herrscherd/herrscher-claude-backend"
)

const (
	probeRecordEnv = "VENDORTEST_PROBE_RECORD"
	probeRecorder  = `#!/bin/sh
if [ "$1" = auth ]; then
	echo '{"loggedIn":true}'
	exit 0
fi
printf '%s\n' "$@" > "$VENDORTEST_PROBE_RECORD"
echo '{"is_error":false}'
`
)

func backendProbeArgs(tb testing.TB) []string {
	tb.Helper()
	dir := tb.TempDir()
	binary := filepath.Join(dir, "claude")
	record := filepath.Join(dir, "argv")
	if err := os.WriteFile(binary, []byte(probeRecorder), 0o700); err != nil {
		tb.Fatal(err)
	}
	tb.Setenv(probeRecordEnv, record)
	report := claudeBackendPlugin(tb).Probe(context.Background(), contracts.ProbeAccess{
		Config: contracts.PluginConfig{Settings: map[string]string{"cmd": binary}},
		Deep:   true,
	})
	if report.State != contracts.HealthOK {
		tb.Fatalf("the backend probe reported %v (%+v), want ok", report.State, report.Detail)
	}
	recorded, err := os.ReadFile(record)
	if err != nil {
		tb.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(recorded), "\n"), "\n")
}

func claudeBackendPlugin(tb testing.TB) contracts.Plugin {
	tb.Helper()
	backends := contracts.Default.Backends()
	for i := range backends {
		if backends[i].Manifest.Kind == "claude" {
			return backends[i]
		}
	}
	tb.Fatal("the claude backend is not registered")
	return contracts.Plugin{}
}

func TestOneShotConsumesNoTurnAndSpawnsNothing(t *testing.T) {
	files := scriptFiles(t, "spawn descendant.pid\ntext Voici\n")
	args := backendProbeArgs(t)
	r, err := openReplay(files, args)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if callErr := r.oneShot(&out, args); callErr != nil {
		t.Fatal(callErr)
	}
	if _, statErr := os.Stat(filepath.Join(filepath.Dir(files.Log), "descendant.pid")); !os.IsNotExist(statErr) {
		t.Fatalf("a -p call spawned a descendant: stat err %v", statErr)
	}
	turn, err := r.nextTurn()
	if err != nil {
		t.Fatal(err)
	}
	if len(turn.spawns) != 1 || len(turn.steps) != 1 {
		t.Fatalf("the first turn was consumed by -p: got %+v", turn)
	}
}

func TestHealthProbeIsRecognisedByItsArgv(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want bool
	}{
		{"the backend probe", backendProbeArgs(t), true},
		{"a bare -p prompt", []string{"-p", "say ping please"}, false},
		{"a -p prompt with tools only", []string{"-p", "say ping please", "--tools", ""}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isHealthProbe(tc.args); got != tc.want {
				t.Fatalf("isHealthProbe(%q) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}
