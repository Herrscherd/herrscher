package host

import (
	"context"
	"errors"
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
