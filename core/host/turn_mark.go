package host

import (
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

const turnInterruptedKind = "turn_interrupted"

func renderTurnInterrupted(lang i18n.Language) (string, error) {
	if lang != i18n.English && lang != i18n.French {
		lang = i18n.English
	}
	return contracts.NewRenderer().Render(lang, contracts.Notice{Code: contracts.CodeTurnInterrupted})
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

func (h *hub) announceInterruptedTurn(sess state.Session) {
	if sess.InterruptedTurn == "" {
		return
	}
	lang := h.sessionLanguageSource(sess.Name)
	text, err := renderTurnInterrupted(lang())
	if err != nil {
		warnLive(h.warnings, lang, "warn.session_transcript", map[string]any{nameParam: sess.Name, detailParam: err})
	}
	entry := state.TranscriptEntry{
		Ts:   time.Now().UTC().Format(time.RFC3339),
		Role: "notice",
		Kind: turnInterruptedKind,
		Text: text,
	}
	if err := state.AppendTranscript(state.TranscriptPath(h.partDir, sess.Name), entry); err != nil {
		warnLive(h.warnings, lang, "warn.session_transcript", map[string]any{nameParam: sess.Name, detailParam: err})
		return
	}
	h.clearTurnMark(sess.Name)
}
