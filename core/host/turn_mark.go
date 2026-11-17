package host

import (
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

const turnInterruptedKind = "turn_interrupted"

func renderTurnInterrupted(lang i18n.Language) (string, error) {
	return contracts.NewRenderer().Render(effectiveLanguage(lang), contracts.Notice{Code: contracts.CodeTurnInterrupted})
}

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
	h.announceInterruptedTurnWith(sess, renderTurnInterrupted)
}

func (h *hub) announceInterruptedTurnWith(sess state.Session, render func(i18n.Language) (string, error)) {
	if sess.InterruptedTurn == "" {
		return
	}
	if h.interruptionAnnounced(sess.Name, sess.InterruptedTurn) {
		h.clearTurnMark(sess.Name)
		return
	}
	lang := h.sessionLanguageSource(sess.Name)
	text, err := render(lang())
	if err != nil {
		warnLive(h.warnings, lang, "warn.session_transcript", map[string]any{nameParam: sess.Name, detailParam: err})
		return
	}
	entry := state.TranscriptEntry{
		Ts:     time.Now().UTC().Format(time.RFC3339),
		Role:   "notice",
		Kind:   turnInterruptedKind,
		Text:   text,
		TurnID: sess.InterruptedTurn,
	}
	if err := h.appendTranscript(sess.Name, entry); err != nil {
		warnLive(h.warnings, lang, "warn.session_transcript", map[string]any{nameParam: sess.Name, detailParam: err})
		return
	}
	h.clearTurnMark(sess.Name)
}
