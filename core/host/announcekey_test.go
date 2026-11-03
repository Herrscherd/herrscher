package host

import (
	"os"
	"testing"
)

const testAnnounceSecret = "0123456789abcdef0123456789abcdef"

func resetAnnounceCapture(t *testing.T) {
	t.Helper()
	reset := func() {
		capturedAnnounce.mu.Lock()
		defer capturedAnnounce.mu.Unlock()
		capturedAnnounce.secret = ""
		capturedAnnounce.captured = false
	}
	reset()
	t.Cleanup(reset)
}

func TestCaptureAnnounceKeyRemovesItFromTheEnvironment(t *testing.T) {
	resetAnnounceCapture(t)
	env := map[string]string{envAnnounceKey: testAnnounceSecret}
	captureAnnounceKey(
		func(k string) string { return env[k] },
		func(k string) error { delete(env, k); return nil },
	)
	if _, still := env[envAnnounceKey]; still {
		t.Fatal("the announce key survived the capture: every vendor CLI would inherit it")
	}
	want := []string{envAnnounceKey + "=" + testAnnounceSecret}
	if got := AnnounceKeyEnvPairs(); len(got) != 1 || got[0] != want[0] {
		t.Fatalf("AnnounceKeyEnvPairs() = %v, want %v for trusted children", got, want)
	}
}

func TestAnnounceKeyEnvPairsEmptyWhenUnset(t *testing.T) {
	resetAnnounceCapture(t)
	captureAnnounceKey(func(string) string { return "" }, func(string) error { return nil })
	if got := AnnounceKeyEnvPairs(); len(got) != 0 {
		t.Fatalf("AnnounceKeyEnvPairs() = %v, want empty", got)
	}
}

func TestCaptureAnnounceKeyLeavesTheProcessEnvironmentClean(t *testing.T) {
	resetAnnounceCapture(t)
	t.Setenv(envAnnounceKey, testAnnounceSecret)
	CaptureAnnounceKey()
	if _, set := os.LookupEnv(envAnnounceKey); set {
		t.Fatal("the process environment still carries the announce key after the capture")
	}
}
