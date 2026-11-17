package host

import (
	"context"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/runtimedir"
)

func serveTestCommandSocket(ctx context.Context, t *testing.T, layout runtimedir.Layout, path string, disp dispatcher, readTimeout time.Duration) {
	t.Helper()
	listener, err := openSocket(ctx, newHostMessages(), layout, path)
	if err != nil {
		t.Fatalf("open command socket: %v", err)
	}
	acceptCommands(ctx, listener, disp, readTimeout, time.Second, func(time.Duration) {})
}
