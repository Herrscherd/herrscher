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
	frame := mustResult(t, want)
	got, err := DecodeResult(frame)
	if err != nil {
		t.Fatalf("DecodeResult: %v", err)
	}
	if got.TokensAfter != want.TokensAfter || got.TokensMeasured != want.TokensMeasured || got.Note != want.Note || got.Notice.Code != 0 || len(got.Notice.Args) != 0 {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
}

func TestResultCarriesTypedNotice(t *testing.T) {
	want := contracts.CompactResult{
		TokensAfter:    12_000,
		TokensMeasured: true,
		Notice: contracts.Notice{Code: contracts.CodeSettingInvalid, Args: []contracts.Arg{
			contracts.Text("Setting", "compactAt"),
			contracts.Count(3),
			contracts.Ref("Channel", "4242"),
		}},
	}
	got, err := DecodeResult(mustResult(t, want))
	if err != nil {
		t.Fatalf("DecodeResult: %v", err)
	}
	if got.Notice.Code != contracts.CodeSettingInvalid || len(got.Notice.Args) != 3 {
		t.Fatalf("notice = %+v, want setting.invalid with three arguments", got.Notice)
	}
	text, count, ref := got.Notice.Args[0], got.Notice.Args[1], got.Notice.Args[2]
	if text.Name() != "Setting" || text.Kind() != contracts.ArgText || text.Value() != "compactAt" {
		t.Fatalf("text argument = %s %d %v", text.Name(), text.Kind(), text.Value())
	}
	if count.Name() != "Count" || count.Kind() != contracts.ArgCount || count.Value() != 3 {
		t.Fatalf("count argument = %s %d %v", count.Name(), count.Kind(), count.Value())
	}
	if ref.Name() != "Channel" || ref.Kind() != contracts.ArgRef || ref.Value() != "4242" {
		t.Fatalf("ref argument = %s %d %v", ref.Name(), ref.Kind(), ref.Value())
	}
}

func TestResultUnmeasuredKeepsFlag(t *testing.T) {
	got, err := DecodeResult(mustResult(t, contracts.CompactResult{TokensMeasured: false}))
	if err != nil {
		t.Fatalf("DecodeResult: %v", err)
	}
	if got.TokensMeasured {
		t.Fatal("an unmeasured result decoded as measured")
	}
}

func TestDecodeResultRefusesMalformedNotice(t *testing.T) {
	for _, value := range []string{"{", `{"code":1,"args":[{"name":"X","kind":9,"value":"a"}]}`} {
		if _, err := DecodeResult(contracts.Event{T: EventResult, Value: value}); err == nil {
			t.Fatalf("DecodeResult(%q) accepted a malformed notice", value)
		}
	}
}

func mustResult(t *testing.T, res contracts.CompactResult) contracts.Event {
	t.Helper()
	frame, err := Result(res)
	if err != nil {
		t.Fatalf("Result: %v", err)
	}
	return frame
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
