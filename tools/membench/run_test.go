package main

import (
	"bytes"
	"context"
	"testing"
)

func TestRunExitCodeReflectsTheStatus(t *testing.T) {
	skipOffUnix(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	binary := fakeBinary(t)
	tests := []struct {
		name        string
		interrupted bool
		args        []string
		want        exitCode
	}{
		{name: "interrupted", interrupted: true, args: budgetArgs("-herrscher", binary), want: 4},
		{name: "budget stop", args: []string{"-herrscher", binary, "-budget-tokens", "1", "-reserve-tokens", "1"}, want: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tt.interrupted {
				cancel()
			}
			var stdout, stderr bytes.Buffer
			if got := run(ctx, tt.args, &stdout, &stderr, noEnv); got != tt.want {
				t.Errorf("exit code %d, want %d; report %q, stderr %q", got, tt.want, stdout.String(), stderr.String())
			}
		})
	}
}
