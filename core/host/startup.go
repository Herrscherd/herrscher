package host

import "os"

// CaptureStartupSecrets loads the env file, captures the gateway and announce secrets, and hides the gateway keys.
func CaptureStartupSecrets(load func() error, hide func()) error {
	return captureStartupSecrets(load, hide, os.Getenv, os.Unsetenv)
}

func captureStartupSecrets(load func() error, hide func(), getenv func(string) string, unsetenv func(string) error) error {
	if err := load(); err != nil {
		return err
	}
	captureGatewayCreds(getenv, unsetenv)
	captureAnnounceKey(getenv, unsetenv)
	hide()
	return nil
}
