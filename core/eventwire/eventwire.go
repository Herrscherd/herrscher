// Package eventwire names the event types that cross the session bus between the host, the bridge and the gateways: the value of contracts.Event.T.
package eventwire

// Event types carried in contracts.Event.T: a turn's output and progress, the controls a gateway sends down, and the approval, menu and compaction notices.
const (
	EventChunk             = "chunk"
	EventThinking          = "thinking"
	EventStatus            = "status"
	EventToolDone          = "tool_done"
	EventTodos             = "todos"
	EventSubagent          = "subagent"
	EventReset             = "reset"
	EventReply             = "reply"
	EventAbandoned         = "abandoned"
	EventClosed            = "closed"
	EventNotice            = "notice"
	EventBackendNotice     = "backend_notice"
	EventInput             = "input"
	EventHuman             = "human"
	EventPaused            = "paused"
	EventInterrupt         = "interrupt"
	EventPick              = "pick"
	EventLanguage          = "language"
	EventMenu              = "menu"
	EventApproval          = "approval"
	EventApprovalRisk      = "approval_risk"
	EventApprovalSubject   = "approval_subject"
	EventApprovalAsk       = "approval_ask"
	EventApprovalAnswer    = "approval_answer"
	EventCompactNow        = "compact_now"
	EventCompactionStarted = "compaction_started"
	EventCompactionDone    = "compaction_done"
	EventCompactionFailed  = "compaction_failed"
	EventGap               = "gap"
	EventCaughtUp          = "caught_up"
)

// Values of a gap, caught_up or closed event on the events socket: why a subscriber missed events, the state of the turn once its catch-up is over, and a session that lives on under another name.
const (
	GapExpired      = "expired"
	GapDropped      = "dropped"
	CaughtUpIdle    = "idle"
	CaughtUpRunning = "running"
	CaughtUpEmpty   = "empty"
	ClosedRenamed   = "renamed"
)
