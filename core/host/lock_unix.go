//go:build unix

package host

import (
	"errors"

	"github.com/Herrscherd/herrscher/core/internal/runtimedir"
)

func lockInstance(messages *hostMessages, layout runtimedir.Layout, instanceID string) (func(), error) {
	path := layout.Lock(instanceID)
	if err := ensureRuntimeDir(messages, layout, path); err != nil {
		return nil, err
	}
	release, err := takeLock(messages, path)
	if _, ok := errors.AsType[*lockTakenError](err); ok {
		return nil, wrapHost(messages, errInstanceLocked, "daemon.instance_locked", map[string]any{pathParam: path})
	}
	return release, err
}
