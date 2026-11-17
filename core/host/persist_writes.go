package host

import (
	"context"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

const persistWriteGrace = 2 * time.Second

func (h *hub) appendTranscript(session string, entry state.TranscriptEntry) error {
	end := h.persists.Begin()
	defer end()
	return state.AppendTranscript(state.TranscriptPath(h.partDir, session), entry)
}

func (h *hub) awaitPersists() {
	budget, cancel := context.WithTimeout(context.Background(), persistWriteGrace)
	defer cancel()
	select {
	case <-h.persists.settled():
	case <-budget.Done():
		h.warnPersistOverrun()
	}
}

func (h *hub) warnPersistOverrun() {
	lang := h.textLanguage()()
	params := map[string]any{graceParam: persistWriteGrace.String()}
	logCleanup(h.sink.announce(newHostMessages().text(lang, "warn.persist_overrun", params)))
}
