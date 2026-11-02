package envx

import (
	"log/slog"
	"os"
	"strings"
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

// Hide unsets the given environment variables and returns a function that restores them.
func Hide(keys []string) func() {
	hidden.mu.Lock()
	defer hidden.mu.Unlock()
	if hidden.vals == nil {
		hidden.vals = make(map[string]string)
	}
	prior := make(map[string]priorKey)
	for _, k := range keys {
		if isEnvKey(k) {
			hideKey(k, prior)
		}
	}
	return func() { restoreHidden(prior) }
}

func hideKey(k string, prior map[string]priorKey) {
	env := os.Getenv(k)
	if err := os.Unsetenv(k); err != nil {
		return
	}
	if _, seen := prior[k]; !seen {
		stored, held := hidden.vals[k]
		prior[k] = priorKey{held: held, stored: stored, env: env}
	}
	if hidden.vals[k] == "" {
		hidden.vals[k] = env
	}
}

func restoreHidden(prior map[string]priorKey) {
	hidden.mu.Lock()
	defer hidden.mu.Unlock()
	for k, p := range prior {
		if p.held {
			hidden.vals[k] = p.stored
		} else {
			delete(hidden.vals, k)
		}
		if p.env == "" {
			continue
		}
		if err := os.Setenv(k, p.env); err != nil {
			slog.Error("restore hidden environment variable", "key", k, "err", err)
		}
	}
}

func isEnvKey(k string) bool {
	return k != "" && !strings.ContainsAny(k, "=\x00")
}

// Revealed returns a lookup that prefers hidden values and falls back to fallback.
func Revealed(fallback func(string) string) func(string) string {
	return func(name string) string { return revealed(name, fallback) }
}

// Getenv returns the value of name, preferring a hidden value over the process environment.
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
