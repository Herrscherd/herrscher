package manage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Decision is one operator answer inside a composition change. There are two
// questions and four answers because the two questions are asked at different
// moments: one before anything is written, one after the compiler has spoken.
type Decision int

const (
	// Proceed answers the pre-write warning: write despite the findings.
	Proceed Decision = iota
	// Abort answers the pre-write warning: change nothing.
	Abort
	// Restore answers a failed build: put the tree back as it was.
	Restore
	// Keep answers a failed build: leave the tree broken for a hand repair.
	Keep
)

type decider interface {
	Warn(ctx context.Context, findings []string) Decision
	Failed(ctx context.Context, buildOutput string) Decision
}

type autoDecider struct{}

func (autoDecider) Warn(context.Context, []string) Decision { return Proceed }
func (autoDecider) Failed(context.Context, string) Decision { return Restore }

func newAutoDecider() decider { return autoDecider{} }

type step func(ctx context.Context) (output string, err error)

func composed() []string {
	return []string{"go.mod", "go.sum", "plugins.go"}
}

func apply(ctx context.Context, dir string, findings []string, d decider, steps []step) (bool, error) {
	if d.Warn(ctx, findings) == Abort {
		return false, nil
	}

	saved, err := saveComposition(dir)
	if err != nil {
		return false, err
	}

	for _, s := range steps {
		out, err := s(ctx)
		if err == nil {
			continue
		}
		failure := fmt.Errorf("%w\n%s", err, out)
		if d.Failed(ctx, out) == Restore {
			if rerr := restoreComposition(dir, saved); rerr != nil {
				return false, errors.Join(failure, rerr)
			}
		}
		return false, failure
	}
	return true, nil
}

func saveComposition(dir string) (map[string][]byte, error) {
	saved := make(map[string][]byte, len(composed()))
	for _, name := range composed() {
		body, err := os.ReadFile(filepath.Join(dir, name))
		switch {
		case errors.Is(err, os.ErrNotExist):
			saved[name] = nil
		case err != nil:
			return nil, fmt.Errorf("save %s: %w", name, err)
		default:
			saved[name] = body
		}
	}
	return saved, nil
}

func restoreComposition(dir string, saved map[string][]byte) error {
	for _, name := range composed() {
		path := filepath.Join(dir, name)
		body, ok := saved[name]
		if !ok {
			continue
		}
		var err error
		if body == nil {
			err = os.Remove(path)
			if errors.Is(err, os.ErrNotExist) {
				err = nil
			}
		} else {
			err = os.WriteFile(path, body, 0o644)
		}
		if err != nil {
			return fmt.Errorf("restore %s: %w", name, err)
		}
	}
	return nil
}
