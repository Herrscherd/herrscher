package host

import (
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/eventwire"
)

type invalidArgsNotice struct {
	cause error
}

func (e invalidArgsNotice) Error() string {
	return "notice with undeclared arguments"
}

func (e invalidArgsNotice) Unwrap() error {
	return e.cause
}

func (e invalidArgsNotice) Notice() contracts.Notice {
	return contracts.Notice{Code: contracts.CodeCompactUnavailable, Args: []contracts.Arg{contracts.Text("Bogus", "x")}}
}

func TestNoticeWithInvalidArgumentsFallsBackToTheReason(t *testing.T) {
	driver := &sessionDriver{name: "api", messages: newHostMessages()}
	event := driver.compactionFailure(contracts.TriggerManual, invalidArgsNotice{cause: errBridgeGone})
	payload, err := eventwire.DecodeCompactionFailure(event.Value)
	if err != nil {
		t.Fatalf("decode compaction failure: %v", err)
	}
	if payload.Notice != nil {
		t.Fatalf("payload notice = %+v, want none for a notice that does not render", payload.Notice)
	}
	if payload.Reason != eventwire.CompactReasonBridgeGone {
		t.Fatalf("payload reason = %q, want %q", payload.Reason, eventwire.CompactReasonBridgeGone)
	}
}
