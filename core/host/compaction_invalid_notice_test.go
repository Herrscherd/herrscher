package host

import (
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/eventwire"
)

type invalidArgsNoticeError struct {
	cause error
}

func (e invalidArgsNoticeError) Error() string {
	return "notice with undeclared arguments"
}

func (e invalidArgsNoticeError) Unwrap() error {
	return e.cause
}

func (e invalidArgsNoticeError) Notice() contracts.Notice {
	return contracts.Notice{Code: contracts.CodeCompactUnavailable, Args: []contracts.Arg{contracts.Text("Bogus", "x")}}
}

func TestNoticeWithInvalidArgumentsFallsBackToTheReason(t *testing.T) {
	driver := &sessionDriver{name: "api", messages: newHostMessages()}
	event := driver.compactionFailure(contracts.TriggerManual, invalidArgsNoticeError{cause: errBridgeGone})
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

func TestNoticeMustRenderInEverySupportedLanguage(t *testing.T) {
	notice := contracts.Notice{Code: contracts.CodeTurnInterrupted}
	if !deliverableNotice(notice) {
		t.Fatal("the interruption notice must render in every supported language")
	}
	withUnsupported := append(i18n.Languages(), i18n.Language(255))
	if rendersInEvery(notice, withUnsupported) {
		t.Fatal("a notice that one language cannot render must not be delivered")
	}
}
