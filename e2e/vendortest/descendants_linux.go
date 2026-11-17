//go:build linux

package vendortest

import (
	"errors"
	"os"
	"os/exec"
)

func (r *replay) stopDescendants() error {
	var failures []error
	for _, child := range r.descendants {
		if err := stopDescendant(child); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func stopDescendant(child *exec.Cmd) error {
	if err := child.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	if err := child.Wait(); err != nil {
		if _, killed := errors.AsType[*exec.ExitError](err); !killed {
			return err
		}
	}
	return nil
}
