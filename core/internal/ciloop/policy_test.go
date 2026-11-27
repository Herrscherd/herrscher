package ciloop

import (
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestDecideTable(t *testing.T) {
	open := contracts.PullRequestOpen
	cases := []struct {
		name     string
		snapshot Snapshot
		want     Action
	}{
		{"failed with budget left asks for a fix", Snapshot{State: open, Checks: contracts.ChecksFailed}, ActionFix},
		{"failed with the budget spent stops", Snapshot{State: open, Checks: contracts.ChecksFailed, Attempts: MaxAttempts}, ActionStop},
		{"failed after the last budget step still stops", Snapshot{State: open, Checks: contracts.ChecksFailed, Attempts: MaxAttempts + 1}, ActionStop},
		{"failed for a head already reported waits", Snapshot{State: open, Checks: contracts.ChecksFailed, Attempts: 1, Reported: true}, ActionWait},
		{"failed and reported at the spent budget waits", Snapshot{State: open, Checks: contracts.ChecksFailed, Attempts: MaxAttempts, Reported: true}, ActionWait},
		{"failed on a draft waits for a person", Snapshot{State: open, Draft: true, Checks: contracts.ChecksFailed}, ActionHuman},
		{"action required waits for a person", Snapshot{State: open, Checks: contracts.ChecksActionRequired}, ActionHuman},
		{"pending checks wait without an attempt", Snapshot{State: open, Checks: contracts.ChecksPending}, ActionWait},
		{"no check yet waits without an attempt", Snapshot{State: open, Checks: contracts.ChecksNone}, ActionWait},
		{"unreported checks wait without an attempt", Snapshot{State: open, Checks: contracts.ChecksUnknown}, ActionWait},
		{"passed checks are idle", Snapshot{State: open, Checks: contracts.ChecksPassed}, ActionIdle},
		{"a merged request is idle even when red", Snapshot{State: contracts.PullRequestMerged, Checks: contracts.ChecksFailed}, ActionIdle},
		{"a closed request is idle even when red", Snapshot{State: contracts.PullRequestClosed, Checks: contracts.ChecksFailed}, ActionIdle},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Decide(tc.snapshot); got != tc.want {
				t.Fatalf("Decide() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLogTailFitsTheForgeLimit(t *testing.T) {
	if LogTailBytes < 1 || LogTailBytes > contracts.MaxJobLogBytes {
		t.Fatalf("LogTailBytes = %d, want within 1 to %d", LogTailBytes, contracts.MaxJobLogBytes)
	}
	if err := contracts.ValidateLogLimit(LogTailBytes); err != nil {
		t.Fatal(err)
	}
}
