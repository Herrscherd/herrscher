package host

import (
	"encoding/json"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

const turnInterruptedFallback = "The last message got no reply, send it again if needed."

func interruptedTurnTranscript(t *testing.T) string {
	t.Helper()
	partDir := t.TempDir()
	h, _ := newMarkHub(t, partDir, state.Session{Name: "main", InterruptedTurn: "turn-1"})
	if err := state.AppendTranscript(state.TranscriptPath(partDir, "main"), state.TranscriptEntry{Role: "user", Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	h.announceInterruptedTurn(state.Session{Name: "main", InterruptedTurn: "turn-1"})
	return partDir
}

func TestInterruptedTurnScrollbackLineCarriesTheNotice(t *testing.T) {
	partDir := interruptedTurnTranscript(t)
	h := &hub{partDir: partDir}

	lines := h.Scrollback("main")
	if len(lines) != 2 {
		t.Fatalf("scrollback = %+v, want the user line and the interruption", lines)
	}
	if lines[0].Notice != nil || lines[0].Text != "hi" {
		t.Fatalf("user line = %+v, want text hi and no notice", lines[0])
	}
	interrupted := lines[1]
	if interrupted.Notice == nil || interrupted.Notice.Code != contracts.CodeTurnInterrupted || interrupted.Notice.Args != nil {
		t.Fatalf("interruption notice = %+v, want CodeTurnInterrupted without arguments", interrupted.Notice)
	}
	if interrupted.Text != turnInterruptedFallback {
		t.Fatalf("interruption text = %q, want the fallback %q", interrupted.Text, turnInterruptedFallback)
	}
}

func TestSessionScrollbackJSONKeepsTheNotice(t *testing.T) {
	partDir := interruptedTurnTranscript(t)

	result, err := sessionScrollbackVerb(partDir).Run(t.Context(), contracts.Input{Args: map[string]string{paramName: "main"}})
	if err != nil {
		t.Fatalf("session scrollback: %v", err)
	}
	raw, ok := result.Data().(string)
	if !ok {
		t.Fatalf("session scrollback data = %T, want a JSON string", result.Data())
	}
	var lines []contracts.ScrollbackLine
	if err := json.Unmarshal([]byte(raw), &lines); err != nil {
		t.Fatalf("session scrollback JSON: %v", err)
	}
	if len(lines) != 2 || lines[1].Notice == nil || lines[1].Notice.Code != contracts.CodeTurnInterrupted {
		t.Fatalf("session scrollback = %s, want the interruption line to keep CodeTurnInterrupted", raw)
	}
	if strings.Count(raw, `"Notice"`) != 1 {
		t.Fatalf("session scrollback = %s, want a notice only on the interruption line", raw)
	}
}
