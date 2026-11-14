package eventwire

import "testing"

func TestEventValuesAreFrozen(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"EventChunk", EventChunk, "chunk"},
		{"EventThinking", EventThinking, "thinking"},
		{"EventStatus", EventStatus, "status"},
		{"EventTodos", EventTodos, "todos"},
		{"EventSubagent", EventSubagent, "subagent"},
		{"EventReset", EventReset, "reset"},
		{"EventReply", EventReply, "reply"},
		{"EventAbandoned", EventAbandoned, "abandoned"},
		{"EventClosed", EventClosed, "closed"},
		{"EventNotice", EventNotice, "notice"},
		{"EventInput", EventInput, "input"},
		{"EventHuman", EventHuman, "human"},
		{"EventPaused", EventPaused, "paused"},
		{"EventInterrupt", EventInterrupt, "interrupt"},
		{"EventPick", EventPick, "pick"},
		{"EventLanguage", EventLanguage, "language"},
		{"EventMenu", EventMenu, "menu"},
		{"EventApproval", EventApproval, "approval"},
		{"EventApprovalRisk", EventApprovalRisk, "approval_risk"},
		{"EventApprovalSubject", EventApprovalSubject, "approval_subject"},
		{"EventCompactNow", EventCompactNow, "compact_now"},
		{"EventCompactionStarted", EventCompactionStarted, "compaction_started"},
		{"EventCompactionDone", EventCompactionDone, "compaction_done"},
		{"EventCompactionFailed", EventCompactionFailed, "compaction_failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
			}
		})
	}
}
