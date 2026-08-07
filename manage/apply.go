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

// Decider is the seam that lets one transaction serve both front ends: the CLI
// implements it with a terminal prompt, the TUI with a select menu, and neither
// owns any of the logic around it.
type Decider interface {
	Warn(ctx context.Context, findings []string) Decision
	Failed(ctx context.Context, buildOutput string) Decision
}

// autoDecider takes the safe branch at both prompts. It is what a run without a
// human answers with: a tool running unattended must not leave a broken tree.
type autoDecider struct{}

func (autoDecider) Warn(context.Context, []string) Decision { return Proceed }
func (autoDecider) Failed(context.Context, string) Decision { return Restore }

// NewAutoDecider returns the non-interactive Decider, used by --yes and by any
// run with no terminal to ask.
func NewAutoDecider() Decider { return autoDecider{} }

// step is one toolchain invocation inside the transaction. It returns the
// command's combined output so a failure can be reported in the compiler's own
// words rather than paraphrased.
type step func(ctx context.Context) (output string, err error)

// composed lists the files a composition change can touch, and therefore the
// files worth saving before it starts.
var composed = []string{"go.mod", "go.sum", "plugins.go"}

// apply runs a composition change as a transaction with the operator inside it:
// warn from what is known, save, try, and ask again when the compiler refuses.
// The steps are injected so the whole mechanism is exercisable without a
// compiler and without a terminal.
func apply(ctx context.Context, dir string, findings []string, d Decider, steps []step) error {
	if d.Warn(ctx, findings) == Abort {
		return nil
	}

	saved, err := saveComposition(dir)
	if err != nil {
		return err
	}

	for _, s := range steps {
		out, err := s(ctx)
		if err == nil {
			continue
		}
		failure := fmt.Errorf("%w\n%s", err, out)
		if d.Failed(ctx, out) == Restore {
			if rerr := restoreComposition(dir, saved); rerr != nil {
				return errors.Join(failure, rerr)
			}
		}
		return failure
	}
	return nil
}

// saveComposition copies the composition files into memory. A file that does not
// exist is saved as absent rather than skipped, so a go.sum the attempt creates
// does not survive a restore.
func saveComposition(dir string) (map[string][]byte, error) {
	saved := make(map[string][]byte, len(composed))
	for _, name := range composed {
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
	for _, name := range composed {
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
