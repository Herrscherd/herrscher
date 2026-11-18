// Package eventwire names the event types that cross the session bus between the host, the bridge and the gateways: the value of contracts.Event.T.
package eventwire

// Event types carried in contracts.Event.T: a turn's output and progress, the controls a gateway sends down, and the approval, menu and compaction notices.
const (
	EventChunk             = "chunk"
	EventThinking          = "thinking"
	EventStatus            = "status"
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
	EventCompactNow        = "compact_now"
	EventCompactionStarted = "compaction_started"
	EventCompactionDone    = "compaction_done"
	EventCompactionFailed  = "compaction_failed"
)
