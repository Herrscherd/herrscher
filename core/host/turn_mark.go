package host

import (
	"time"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

const turnInterruptedKind = "turn_interrupted"

func (h *hub) openTurnMark(session, turnID string) {
	end := h.persists.begin()
	defer end()
	if err := h.st.SetInterruptedTurn(session, turnID); err != nil {
		warnLive(h.messages, h.warnings, h.sessionLanguageSource(session), "warn.session_turn_mark_set", map[string]any{nameParam: session, detailParam: err})
	}
}

func (h *hub) clearTurnMark(session string) {
	end := h.persists.begin()
	defer end()
	if err := h.st.SetInterruptedTurn(session, ""); err != nil {
		warnLive(h.messages, h.warnings, h.sessionLanguageSource(session), "warn.session_turn_mark_clear", map[string]any{nameParam: session, detailParam: err})
	}
}

func (h *hub) interruptionAnnounced(name, turnID string) bool {
	entries := state.ReadTranscriptTail(state.TranscriptPath(h.partDir, name), scrollbackCap)
	for i := range entries {
		if entries[i].Kind == turnInterruptedKind && entries[i].TurnID == turnID {
			return true
		}
	}
	return false
}

func (h *hub) announceInterruptedTurn(sess state.Session) {
	if sess.InterruptedTurn == "" {
		return
	}
	if h.interruptionAnnounced(sess.Name, sess.InterruptedTurn) {
		h.clearTurnMark(sess.Name)
		return
	}
	entry := state.TranscriptEntry{
		Ts:     time.Now().UTC().Format(time.RFC3339),
		Role:   "notice",
		Kind:   turnInterruptedKind,
		TurnID: sess.InterruptedTurn,
	}
	if err := h.appendTranscript(sess.Name, entry); err != nil {
		lang := h.sessionLanguageSource(sess.Name)
		warnLive(h.messages, h.warnings, lang, "warn.session_transcript", map[string]any{nameParam: sess.Name, detailParam: err})
		return
	}
	h.clearTurnMark(sess.Name)
}
