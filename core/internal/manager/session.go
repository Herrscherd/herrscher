package manager

import (
	"time"
)

const cloneTimeout = 10 * time.Minute

const maxSessions = 64

const sessionLogTranscriptCap = 200

const (
	nameParam       = "name"
	terminalPlace   = "terminal"
	categoryKind    = "category"
	forumKind       = "forum"
	nonNegativeHint = "use a non-negative integer"
)

func (h *Handler) warnIfFailed(err error, action string) {
	if err != nil {
		h.log.Warn("best effort step failed", "action", action, "err", err)
	}
}
