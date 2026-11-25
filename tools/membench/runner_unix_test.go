//go:build unix

package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestRunReturnsWhenAGrandchildHoldsTheOutputPipe(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "fake")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nsleep 30 &\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	box, err := newSandbox(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := runner{binary: binary, model: defaultModel}
	started := time.Now()
	_, invokeErr := r.invoke(context.Background(), box, step{agent: agentAlpha, prompt: "x"}, nil)
	if elapsed := time.Since(started); elapsed > 20*time.Second {
		t.Fatalf("invoke held the output pipe for %s, the wait delay is %s", elapsed, pipeWaitDelay)
	}
	if !errors.Is(invokeErr, exec.ErrWaitDelay) {
		t.Errorf("invoke returned %v, want the wait-delay error", invokeErr)
	}
}
