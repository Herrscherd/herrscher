package main

import (
	"context"
	"testing"

	"github.com/Herrscherd/herrscher/core/host"
)

func TestRunBridgeHelpSucceeds(t *testing.T) {
	if err := runBridge(context.Background(), testMessages(), host.NewMessages(), []string{"-h"}); err != nil {
		t.Fatalf("-h must succeed, got %v", err)
	}
}

func TestRunBridgeRejectsUnknownFlag(t *testing.T) {
	if err := runBridge(context.Background(), testMessages(), host.NewMessages(), []string{"-no-such-flag"}); err == nil {
		t.Fatal("an unknown flag must be an error")
	}
}
