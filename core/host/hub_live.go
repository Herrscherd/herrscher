package host

import (
	"context"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/usage"
)

func eventTap(publish func(string, contracts.Event), session string) func(contracts.Event) {
	if publish == nil {
		return nil
	}
	return func(e contracts.Event) { publish(session, e) }
}

func (h *hub) liveSink(ctx context.Context, sess state.Session) sessionSink {
	return sessionSink{
		Resume: func(tok string) {
			if err := h.st.SetResumeToken(sess.Name, tok); err != nil {
				warnLive(h.warnings, h.sessionLanguageSource(sess.Name), "warn.session_resume_token", map[string]any{nameParam: sess.Name, detailParam: err})
			}
		},
		Project: func(p string) {
			if err := h.st.SetProjectPinned(sess.Name, p); err != nil {
				warnLive(h.warnings, h.sessionLanguageSource(sess.Name), "warn.session_project_pin", map[string]any{nameParam: sess.Name, detailParam: err})
			}
		},
		Transcript: func(e state.TranscriptEntry) {
			if err := state.AppendTranscript(state.TranscriptPath(h.partDir, sess.Name), e); err != nil {
				warnLive(h.warnings, h.sessionLanguageSource(sess.Name), "warn.session_transcript", map[string]any{nameParam: sess.Name, detailParam: err})
			}
		},
		Checkpoint: func(turnID, text string) {
			h.takeCheckpoint(ctx, sess, turnID, text)
		},
		Usage: func(records []usage.Record) {
			h.recordUsage(sess, records)
		},
	}
}
