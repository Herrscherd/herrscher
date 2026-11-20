package host

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/health"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

const nilWarningsChildEnv = "HERRSCHER_NIL_WARNINGS_CHILD"

func TestNilWarningsReachStderrWithoutPanicking(t *testing.T) {
	if os.Getenv(nilWarningsChildEnv) != "" {
		emitNilWarnings()
		return
	}
	printed := runNilWarningsChild(t)
	for _, marker := range []string{"probe-warnto", "approvals: probe-warnf", "the approval wait of ", "bad addr", "probe-consolidate"} {
		if !strings.Contains(printed, marker) {
			t.Fatalf("stderr = %q, want it to contain %q", printed, marker)
		}
	}
}

func runNilWarningsChild(t *testing.T) string {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd := exec.CommandContext(t.Context(), executable, "-test.run=^TestNilWarningsReachStderrWithoutPanicking$")
	cmd.Env = append(os.Environ(), nilWarningsChildEnv+"=1")
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("nil-writer child failed: %v\n%s", err, stderr.String())
	}
	return stderr.String()
}

func emitNilWarnings() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	consolidated := errors.New("probe-consolidate")
	messages := newHostMessages()

	warnTo(messages, nil, i18n.English, "warn.health_server", map[string]any{detailParam: "probe-warnto"})
	newApprovalTable(nil).warnf("approvals: probe-warnf\n")
	newApprovalTable(nil).warnWaitTruncated(agent.HookWait, hookAnswerWait())
	serveHealth(ctx, "bad addr", health.NewHealth(time.Now()), messages, fixedLanguageSource(i18n.English), nil)
	consolidateOneShot(ctx, &consolidateSpy{err: consolidated}, newSeedGate(messages, "nil-writer", nil, fixedLanguageSource(i18n.English)), state.Session{Name: "nil-writer"}, messages, nil, fixedLanguageSource(i18n.English))
}
