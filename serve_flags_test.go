package main

import (
	"context"
	"testing"

	"github.com/Herrscherd/herrscher/core/host"
)

func TestRunServeRejectsUnknownFlag(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := runServe(context.Background(), testRuntime(t), testMessages(), host.NewMessages(), []string{"-no-such-flag"}, serveDaemon); err == nil {
		t.Fatal("an unknown flag must be an error, returned to main to exit 1")
	}
}
