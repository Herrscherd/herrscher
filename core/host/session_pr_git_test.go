package host

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultBranchReportsAnEndedContextAsTheContextError(t *testing.T) {
	dir := prFixtureRepo(t)
	cases := []struct {
		name  string
		ended func(context.Context) (context.Context, context.CancelFunc)
		want  error
	}{
		{name: "canceled", ended: canceledContext, want: context.Canceled},
		{name: "expired", ended: expiredContext, want: context.DeadlineExceeded},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, end := tc.ended(t.Context())
			t.Cleanup(end)
			_, err := defaultBranch(ctx, dir)
			if errors.Is(err, errPRNoBase) {
				t.Fatalf("defaultBranch under an ended context = %v, want %v, not the missing base", err, tc.want)
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("defaultBranch under an ended context = %v, want %v", err, tc.want)
			}
		})
	}
}

func canceledContext(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	cancel()
	return ctx, cancel
}

func expiredContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithDeadline(parent, time.Now().Add(-time.Second))
}

func TestPRGitReportsAContextEndedBeforeOrDuringTheCommandAsTheContextError(t *testing.T) {
	dir := t.TempDir()
	sleepingGit := t.TempDir()
	if err := os.WriteFile(filepath.Join(sleepingGit, "git"), []byte("#!/bin/sh\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", sleepingGit+string(os.PathListSeparator)+os.Getenv("PATH"))
	cases := []struct {
		name  string
		ended func(context.Context) (context.Context, context.CancelFunc)
		want  error
	}{
		{name: "canceled", ended: canceledContext, want: context.Canceled},
		{name: "expired", ended: expiredContext, want: context.DeadlineExceeded},
		{name: "killed during the command", ended: shortTimeoutContext, want: context.DeadlineExceeded},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, end := tc.ended(t.Context())
			t.Cleanup(end)
			_, err := prGit(ctx, dir, nil, "rev-parse", "HEAD")
			if !errors.Is(err, tc.want) {
				t.Fatalf("prGit under an ended context = %v, want %v", err, tc.want)
			}
		})
	}
}

func shortTimeoutContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, 100*time.Millisecond)
}
