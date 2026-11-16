package host

import (
	"time"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

func (h *hub) openTurnMark(session, turnID string) {
	if err := h.st.SetInterruptedTurn(session, turnID); err != nil {
		warnLive(h.warnings, h.sessionLanguageSource(session), "warn.session_turn_mark_set", map[string]any{nameParam: session, detailParam: err})
	}
}

func (h *hub) clearTurnMark(session string) {
	if err := h.st.SetInterruptedTurn(session, ""); err != nil {
		warnLive(h.warnings, h.sessionLanguageSource(session), "warn.session_turn_mark_clear", map[string]any{nameParam: session, detailParam: err})
	}
}

func (h *hub) announceInterruptedTurn(sess state.Session) {
	if sess.InterruptedTurn == "" {
		return
	}
	lang := h.sessionLanguageSource(sess.Name)
	entry := state.TranscriptEntry{
		Ts:   time.Now().UTC().Format(time.RFC3339),
		Role: "notice",
		Kind: "turn_interrupted",
		Text: newHostMessages().text(lang(), "warn.session_turn_interrupted", nil),
	}
	if err := state.AppendTranscript(state.TranscriptPath(h.partDir, sess.Name), entry); err != nil {
		warnLive(h.warnings, lang, "warn.session_transcript", map[string]any{nameParam: sess.Name, detailParam: err})
		return
	}
	h.clearTurnMark(sess.Name)
}
