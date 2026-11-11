package main

import (
	"context"
	"testing"
)

func TestRunServeRejectsUnknownFlag(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := runServe(context.Background(), testRuntime(t), []string{"-no-such-flag"}, serveDaemon); err == nil {
		t.Fatal("an unknown flag must be an error, returned to main to exit 1")
	}
}
