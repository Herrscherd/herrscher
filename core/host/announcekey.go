package host

import (
	"os"
	"sync"
)

const envAnnounceKey = "HERRSCHER_ANNOUNCE_KEY"

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
		capturedAnnounce.secret = getenv(envAnnounceKey)
		capturedAnnounce.captured = true
	}
	logCleanup(unsetenv(envAnnounceKey))
}

// AnnounceKeySecret returns the captured announce secret, or the environment value when nothing was captured.
func AnnounceKeySecret() string {
	capturedAnnounce.mu.Lock()
	defer capturedAnnounce.mu.Unlock()
	if capturedAnnounce.captured {
		return capturedAnnounce.secret
	}
	return os.Getenv(envAnnounceKey)
}

// AnnounceKeyEnvPairs returns the announce secret as KEY=VALUE for a trusted child only, empty when unset.
func AnnounceKeyEnvPairs() []string {
	secret := AnnounceKeySecret()
	if secret == "" {
		return nil
	}
	return []string{envAnnounceKey + "=" + secret}
}
