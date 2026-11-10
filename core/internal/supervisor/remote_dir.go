package supervisor

import (
	"context"

	"github.com/Herrscherd/herrscher/core/internal/runner"
)

func (s *Supervisor) remoteRuntimeDir(ctx context.Context, target string) (string, error) {
	s.remoteMu.Lock()
	defer s.remoteMu.Unlock()
	if dir, ok := s.remoteDirs[target]; ok {
		return dir, nil
	}
	lookup := s.remoteDirOf
	if lookup == nil {
		lookup = runner.RemoteRuntimeDir
	}
	dir, err := lookup(ctx, target)
	if err != nil {
		return "", err
	}
	if s.remoteDirs == nil {
		s.remoteDirs = make(map[string]string)
	}
	s.remoteDirs[target] = dir
	return dir, nil
}
