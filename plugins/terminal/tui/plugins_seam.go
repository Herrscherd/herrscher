package tui

import (
	"context"

	"github.com/Herrscherd/herrscher/manage"
)

type manageSeam struct{ ops *manage.PluginOps }

func newManageSeam() (PluginSeam, error) {
	ops, err := manage.NewPluginOps("")
	if err != nil {
		return nil, err
	}
	return manageSeam{ops: ops}, nil
}

func (s manageSeam) List(ctx context.Context) ([]PluginRow, error) {
	values, err := s.ops.List(ctx)
	if err != nil {
		return nil, err
	}
	rows := make([]PluginRow, 0, len(values))
	for _, v := range values {
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
