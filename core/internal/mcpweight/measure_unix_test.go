//go:build unix

package mcpweight

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMeasureKillsTheGrandchildOfAServerThatAnswers(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "grandchild.pid")
	result := Measure(context.Background(), fakeServerWithEnv("forking", map[string]string{fakePIDFileEnv: pidPath}), DefaultTimeout)
	if !result.Measured || result.Tools != fakeToolCount {
		t.Fatalf("Measure = %+v, want the tools", result)
	}
	awaitProcessGone(t, readPID(t, pidPath))
}

func TestMeasureKillsTheGrandchildOfAServerThatTimesOut(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "grandchild.pid")
	result := Measure(context.Background(), fakeServerWithEnv("forking-silent", map[string]string{fakePIDFileEnv: pidPath}), 2*time.Second)
	if result.Measured || result.Cause != CauseTimeout {
		t.Fatalf("Measure = %+v, want a timeout", result)
	}
	awaitProcessGone(t, readPID(t, pidPath))
}

func TestMeasureKeepsToolsWhenAGrandchildHoldsTheOutputOpen(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "grandchild.pid")
	result := Measure(context.Background(), fakeServerWithEnv("escaping", map[string]string{fakePIDFileEnv: pidPath}), DefaultTimeout)
	killOnCleanup(t, readPID(t, pidPath))
	if !result.Measured || result.Tools != fakeToolCount || result.Cause != CauseStop {
		t.Fatalf("Measure = %+v, want the tools kept with a stop cause", result)
	}
	if !strings.Contains(result.Detail, errOutputOpen.Error()) {
		t.Fatalf("Detail = %q, want it to report the output still open", result.Detail)
	}
}
