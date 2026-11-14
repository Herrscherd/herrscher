package control_test

import (
	"context"

	"github.com/Herrscherd/herrscher/core/internal/control"
)

func serveSocket(ctx context.Context, path string) (*control.Acceptor, error) {
	ln, err := control.Listen(ctx, path)
	if err != nil {
		return nil, err
	}
	return control.Serve(ctx, ln), nil
}
