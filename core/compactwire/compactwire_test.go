package compactwire

import (
	"errors"
	"testing"
	"time"

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

func TestDecodeResultKeepsTheNoteWhenTheNoticeIsMalformed(t *testing.T) {
	for _, value := range []string{"{", `{"code":1,"args":[{"name":"X","kind":9,"value":"a"}]}`} {
		frame := contracts.Event{T: EventResult, Text: "compacté", Tokens: 42, Done: true, Value: value}
		got, err := DecodeResult(frame)
		if !errors.Is(err, ErrNoticeMalformed) {
			t.Fatalf("DecodeResult(%q) error = %v, want ErrNoticeMalformed", value, err)
		}
		if got.Note != "compacté" || got.TokensAfter != 42 || !got.TokensMeasured || got.Notice.Code != 0 {
			t.Fatalf("DecodeResult(%q) = %+v, want the note and the figures without a notice", value, got)
		}
	}
}

func TestResultCarriesStatsNextToTheNotice(t *testing.T) {
	stats := contracts.CompactStats{
		Stage:         "backend",
		Outcome:       contracts.CompactShort,
		Calls:         4,
		Kept:          1,
		Truncated:     2,
		Pruned:        1,
		Pinned:        3,
		CharsBefore:   9_000,
		CharsAfter:    2_000,
		TokensRemoved: 1_800,
		Requests:      2,
		RequestTokens: 5_000,
		Duration:      1500 * time.Millisecond,
	}
	want := contracts.CompactResult{
		TokensAfter:    12_000,
		TokensMeasured: true,
		Note:           "compacté",
		Notice:         contracts.Notice{Code: contracts.CodeSettingInvalid, Args: []contracts.Arg{contracts.Count(3)}},
		Stats:          &stats,
	}
	got, err := DecodeResult(mustResult(t, want))
	if err != nil {
		t.Fatalf("DecodeResult: %v", err)
	}
	if got.Stats == nil || *got.Stats != stats {
		t.Fatalf("stats = %+v, want %+v", got.Stats, stats)
	}
	if got.Notice.Code != contracts.CodeSettingInvalid || len(got.Notice.Args) != 1 || got.Notice.Args[0].Value() != 3 {
		t.Fatalf("notice = %+v, want setting.invalid with its count", got.Notice)
	}
	if got.Note != want.Note || got.TokensAfter != want.TokensAfter || !got.TokensMeasured {
		t.Fatalf("round trip = %+v, want the note and the figures kept", got)
	}
}

func TestResultStatsWithoutNoticeRoundTrip(t *testing.T) {
	stats := contracts.CompactStats{Stage: "jev", Outcome: contracts.CompactFailed, Duration: time.Second}
	got, err := DecodeResult(mustResult(t, contracts.CompactResult{Stats: &stats}))
	if err != nil {
		t.Fatalf("DecodeResult: %v", err)
	}
	if got.Stats == nil || *got.Stats != stats || got.Notice.Code != 0 {
		t.Fatalf("round trip = %+v, want the stats and no notice", got)
	}
}

func TestResultWithoutStatsDecodesNilStats(t *testing.T) {
	notice := contracts.Notice{Code: contracts.CodeSettingInvalid, Args: []contracts.Arg{contracts.Count(3)}}
	legacy := mustResult(t, contracts.CompactResult{Notice: notice})
	for _, frame := range []contracts.Event{legacy, mustResult(t, contracts.CompactResult{})} {
		got, err := DecodeResult(frame)
		if err != nil {
			t.Fatalf("DecodeResult(%q): %v", frame.Value, err)
		}
		if got.Stats != nil {
			t.Fatalf("DecodeResult(%q) stats = %+v, want nil for a result without stats", frame.Value, got.Stats)
		}
	}
}

func TestResultRefusesStatsWithAnUnknownOutcome(t *testing.T) {
	_, err := Result(contracts.CompactResult{Stats: &contracts.CompactStats{Stage: "jev"}})
	if !errors.Is(err, contracts.ErrUnknownCompactOutcome) {
		t.Fatalf("Result with a zero outcome: %v, want ErrUnknownCompactOutcome", err)
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
