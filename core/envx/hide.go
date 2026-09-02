package envx

import (
	"os"
	"sync"
)

var hidden struct {
	mu   sync.Mutex
	vals map[string]string
}

type priorKey struct {
	held        bool
	stored, env string
}

func Hide(keys []string) (reveal func()) {
	hidden.mu.Lock()
	defer hidden.mu.Unlock()
	if hidden.vals == nil {
		hidden.vals = map[string]string{}
	}
	prior := map[string]priorKey{}
	for _, k := range keys {
		if k == "" {
			continue
		}
		if _, seen := prior[k]; !seen {
			stored, held := hidden.vals[k]
			prior[k] = priorKey{held: held, stored: stored, env: os.Getenv(k)}
		}
		if hidden.vals[k] == "" {
			hidden.vals[k] = os.Getenv(k)
		}
		_ = os.Unsetenv(k)
	}
	return func() {
		hidden.mu.Lock()
		defer hidden.mu.Unlock()
		for k, p := range prior {
			if p.held {
				hidden.vals[k] = p.stored
			} else {
				delete(hidden.vals, k)
			}
			if p.env == "" {
				_ = os.Unsetenv(k)
				continue
			}
			_ = os.Setenv(k, p.env)
		}
	}
}

func Revealed(fallback func(string) string) func(string) string {
	return func(name string) string { return revealed(name, fallback) }
}

func Getenv(name string) string { return revealed(name, os.Getenv) }

func revealed(name string, fallback func(string) string) string {
	hidden.mu.Lock()
	v := hidden.vals[name]
	hidden.mu.Unlock()
	if v != "" {
		return v
	}
	return fallback(name)
}
