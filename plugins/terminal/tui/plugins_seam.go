package tui

import (
	"context"

	"github.com/Herrscherd/herrscher/manage"
)

// manageSeam is the production PluginSeam: the screen's narrow interface bound
// to the host's own management package. It is a separate type so the screen
// itself stays free of the Go toolchain, and so a build with no host module
// beside it simply runs without the screen.
type manageSeam struct{ ops *manage.PluginOps }

// newManageSeam binds the screen to the host module, or reports why it cannot.
func newManageSeam() (PluginSeam, error) {
	ops, err := manage.NewPluginOps("")
	if err != nil {
		return nil, err
	}
	return manageSeam{ops: ops}, nil
}

func (s manageSeam) List(ctx context.Context) ([]PluginRow, error) {
	vs, err := s.ops.List(ctx)
	if err != nil {
		return nil, err
	}
	rows := make([]PluginRow, 0, len(vs))
	for _, v := range vs {
		rows = append(rows, PluginRow{Module: v.Module, Installed: v.Installed, Latest: v.Latest, Pinned: v.Pinned})
	}
	return rows, nil
}

func (s manageSeam) Versions(ctx context.Context, module string) ([]string, error) {
	return s.ops.Versions(ctx, module)
}

func (s manageSeam) Findings(ctx context.Context, module, version string) []string {
	return s.ops.Findings(ctx, module, version)
}

func (s manageSeam) Apply(ctx context.Context, action PluginAction, module, version string) (string, error) {
	return s.ops.Apply(ctx, string(action), module, version)
}

func (s manageSeam) Restore(context.Context) error { return s.ops.Restore() }
