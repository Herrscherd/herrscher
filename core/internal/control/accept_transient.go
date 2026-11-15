package control

import (
	"errors"
	"slices"
)

func isTransientAcceptError(err error) bool {
	return slices.ContainsFunc(transientAcceptErrors(), func(target error) bool {
		return errors.Is(err, target)
	})
}
