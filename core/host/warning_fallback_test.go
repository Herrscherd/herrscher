package host

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/health"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func captureProcessStderr(t *testing.T, emit func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = writer
	defer func() { os.Stderr = old }()

	done := make(chan string)
	go func() {
		var buf strings.Builder
		_, copyErr := io.Copy(&buf, reader)
		logCleanup(copyErr)
		logCleanup(reader.Close())
		done <- buf.String()
	}()
	emit()
	logCleanup(writer.Close())
	os.Stderr = old
	return <-done
}

func TestNilWarningsReachStderrWithoutPanicking(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	consolidated := errors.New("probe-consolidate")

	printed := captureProcessStderr(t, func() {
		warnTo(nil, i18n.English, "warn.health_server", map[string]any{detailParam: "probe-warnto"})
		newApprovalTable(nil).warnf("approvals: probe-warnf\n")
		warnWaitOutlivesTheHook(nil, agent.HookWait)
		serveHealth(ctx, "bad addr", health.NewHealth(time.Now()), fixedLanguageSource(i18n.English), nil)
		consolidateOneShot(ctx, &consolidateSpy{err: consolidated}, state.Session{Name: "nil-writer"}, nil, fixedLanguageSource(i18n.English))
	})

	for _, marker := range []string{"probe-warnto", "approvals: probe-warnf", "approvals: a ", "bad addr", "probe-consolidate"} {
		if !strings.Contains(printed, marker) {
			t.Fatalf("stderr = %q, want it to contain %q", printed, marker)
		}
	}
}
