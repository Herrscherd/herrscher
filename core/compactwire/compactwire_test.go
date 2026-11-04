package compactwire

import (
	"errors"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestRequestRoundTrip(t *testing.T) {
	want := contracts.CompactRequest{
		Trigger:       contracts.TriggerAutomatic,
		UsageMeasured: true,
		TokensUsed:    184_000,
		TokenLimit:    200_000,
		Instruction:   "garde les décisions",
	}
	frame, err := Request(want)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	got, err := DecodeRequest(frame)
	if err != nil {
		t.Fatalf("DecodeRequest: %v", err)
	}
	if got.Trigger != want.Trigger || got.UsageMeasured != want.UsageMeasured ||
		got.TokensUsed != want.TokensUsed || got.TokenLimit != want.TokenLimit || got.Instruction != want.Instruction {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
}

func TestRequestRejectsUnknownTrigger(t *testing.T) {
	if _, err := Request(contracts.CompactRequest{}); !errors.Is(err, contracts.ErrUnknownCompactTrigger) {
		t.Fatalf("Request with zero trigger: %v, want ErrUnknownCompactTrigger", err)
	}
}

func TestResultRoundTrip(t *testing.T) {
	want := contracts.CompactResult{TokensAfter: 12_000, TokensMeasured: true, Note: "compacté"}
	if got := DecodeResult(Result(want)); got != want {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
}

func TestResultUnmeasuredKeepsFlag(t *testing.T) {
	got := DecodeResult(Result(contracts.CompactResult{TokensMeasured: false}))
	if got.TokensMeasured {
		t.Fatal("an unmeasured result decoded as measured")
	}
}

func TestNoPortCarriesCode(t *testing.T) {
	ev := NoPort()
	if ev.T != EventError || ev.Value != NoCompactor {
		t.Fatalf("NoPort = %+v, want error event with code %q", ev, NoCompactor)
	}
}

func TestFailureDecodesMessage(t *testing.T) {
	if err := DecodeFailure(Failure(errors.New("boom"))); err == nil || err.Error() != "boom" {
		t.Fatalf("DecodeFailure = %v, want boom", err)
	}
}
