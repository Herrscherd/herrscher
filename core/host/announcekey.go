package host

import (
	"os"
	"sync"
)

// EnvAnnounceKey names the variable that carries the announce secret.
const EnvAnnounceKey = "HERRSCHER_ANNOUNCE_KEY"

var capturedAnnounce = struct {
	mu       sync.Mutex
	secret   string
	captured bool
}{}

// CaptureAnnounceKey stores the announce secret and unsets it from the process environment.
func CaptureAnnounceKey() {
	captureAnnounceKey(os.Getenv, os.Unsetenv)
}

func captureAnnounceKey(getenv func(string) string, unsetenv func(string) error) {
	capturedAnnounce.mu.Lock()
	defer capturedAnnounce.mu.Unlock()
	if !capturedAnnounce.captured {
		capturedAnnounce.secret = getenv(EnvAnnounceKey)
		capturedAnnounce.captured = true
	}
	logCleanup(unsetenv(EnvAnnounceKey))
}

// AnnounceKeySecret returns the captured announce secret, or the environment value when nothing was captured.
func AnnounceKeySecret() string {
	capturedAnnounce.mu.Lock()
	defer capturedAnnounce.mu.Unlock()
	if capturedAnnounce.captured {
		return capturedAnnounce.secret
	}
	return os.Getenv(EnvAnnounceKey)
}

// AnnounceKeyEnvPairs returns the announce secret as KEY=VALUE for a trusted child only, empty when unset.
func AnnounceKeyEnvPairs() []string {
	secret := AnnounceKeySecret()
	if secret == "" {
		return nil
	}
	return []string{EnvAnnounceKey + "=" + secret}
}
