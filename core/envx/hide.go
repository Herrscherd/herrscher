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
	for _, key := range keys {
		if isEnvKey(key) {
			hideKey(key, prior)
		}
	}
	return func() { restoreHidden(prior) }
}

func hideKey(key string, prior map[string]priorKey) {
	env := os.Getenv(key)
	if err := os.Unsetenv(key); err != nil {
		return
	}
	if _, seen := prior[key]; !seen {
		stored, held := hidden.vals[key]
		prior[key] = priorKey{held: held, stored: stored, env: env}
	}
	if hidden.vals[key] == "" {
		hidden.vals[key] = env
	}
}

func restoreHidden(prior map[string]priorKey) {
	hidden.mu.Lock()
	defer hidden.mu.Unlock()
	for key, saved := range prior {
		if saved.held {
			hidden.vals[key] = saved.stored
		} else {
			delete(hidden.vals, key)
		}
		if saved.env == "" {
			continue
		}
		if err := os.Setenv(key, saved.env); err != nil {
			slog.Error("restore hidden environment variable", "key", key, "err", err)
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
	val := hidden.vals[name]
	hidden.mu.Unlock()
	if val != "" {
		return val
	}
	return fallback(name)
}
