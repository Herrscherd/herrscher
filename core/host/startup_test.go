package host

import "testing"

func TestStartupCapturesTheAnnounceKeyOutOfTheEnvironment(t *testing.T) {
	resetAnnounceCapture(t)
	resetGatewayCapture(t)
	env := map[string]string{envAnnounceKey: testAnnounceSecret}
	err := captureStartupSecrets(
		func() error { return nil },
		func() {},
		func(k string) string { return env[k] },
		func(k string) error { delete(env, k); return nil },
	)
	if err != nil {
		t.Fatalf("startup: %v", err)
	}
	if _, still := env[envAnnounceKey]; still {
		t.Fatal("the announce key must leave the process environment at startup")
	}
	if got := AnnounceKeySecret(); got != testAnnounceSecret {
		t.Fatalf("the announce key must stay captured for trusted children, got %q", got)
	}
}
