package supervisor

import (
	"context"

	"github.com/Herrscherd/herrscher/core/internal/runner"
)

func (s *Supervisor) remoteRuntimeDir(ctx context.Context, target string) (string, error) {
	lookup := s.remoteDirOf
	if lookup == nil {
		lookup = runner.RemoteRuntimeDir
	}
	return lookup(ctx, target)
}
