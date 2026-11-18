package tui

import (
	"strings"
	"time"
)

const (
	roleYou        = "you"
	roleAgent      = "agent"
	roleThinking   = "thinking"
	roleTool       = "tool"
	roleNotice     = "notice"
	roleError      = "error"
	roleCost       = "cost"
	roleScrollback = "scrollback"
)

const hostErrPrefix = "⚠️ "

func hostError(text string) (string, bool) {
	trimmed := strings.TrimSpace(text)
	if rest, ok := strings.CutPrefix(trimmed, hostErrPrefix); ok {
		return strings.TrimSpace(rest), true
	}
	return text, false
}

func statusRole(text string) string {
	if _, isErr := hostError(text); isErr {
		return roleError
	}
	return roleTool
}

type entry struct {
	id          uint64
	role        string
	text        string
	raw         string
	streaming   bool
	attachments []Attachment
	preview     string
	at          time.Time
	ended       time.Time
	base        int
	tokens      int
	span        time.Duration
	live        bool
	failed      bool
	foldable    bool
	open        bool
	markdown    bool
}
