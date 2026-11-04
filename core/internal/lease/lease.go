// Package lease ties a key grant to the lifetime of the backend it serves.
package lease

import (
	"sync"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

type leased struct {
	contracts.Backend

	once    *sync.Once
	release func()
}

// Wrap returns backend with release run once, when it is closed.
func Wrap(backend contracts.Backend, release func()) contracts.Backend {
	return &leased{Backend: backend, once: &sync.Once{}, release: release}
}

func (l *leased) Close() error {
	err := l.Backend.Close()
	l.once.Do(l.release)
	return err
}

// Unwrap returns the backend a Wrap call wrapped, or backend itself.
func Unwrap(backend contracts.Backend) contracts.Backend {
	if wrapped, ok := backend.(*leased); ok {
		return wrapped.Backend
	}
	return backend
}
