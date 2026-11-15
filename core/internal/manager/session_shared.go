package manager

const maxSessions = 64

const sessionLogTranscriptCap = 200

const (
	nameParam     = "name"
	terminalPlace = "terminal"
	categoryKind  = "category"
	forumKind     = "forum"
)

func (h *Handler) warnIfFailed(err error, action string) {
	if err != nil {
		h.log.Warn("best effort step failed", "action", action, "err", err)
	}
}
