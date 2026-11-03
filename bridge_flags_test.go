package main

import (
	"context"
	"testing"
)

func TestRunBridgeHelpSucceeds(t *testing.T) {
	if err := runBridge(context.Background(), []string{"-h"}); err != nil {
		t.Fatalf("-h must succeed, got %v", err)
	}
}

func TestRunBridgeRejectsUnknownFlag(t *testing.T) {
	if err := runBridge(context.Background(), []string{"-no-such-flag"}); err == nil {
		t.Fatal("an unknown flag must be an error")
	}
}
