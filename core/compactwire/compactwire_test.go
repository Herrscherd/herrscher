package compactwire

import (
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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
	for _, value := range []string{"{", `{"code":1,"args":[{"name":"X","kind":1}]}`} {
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

func TestDecodeResultReadsAnOldBackendFrame(t *testing.T) {
	frame := contracts.Event{
		T:      EventResult,
		Text:   "compacté",
		Value:  `{"code":7,"args":[{"name":"Setting","kind":1,"value":"compactAt"},{"name":"Count","kind":2,"value":3}]}`,
		Tokens: 12_000,
		Done:   true,
	}
	got, err := DecodeResult(frame)
	if err != nil {
		t.Fatalf("DecodeResult: %v", err)
	}
	if got.Stats != nil {
		t.Fatalf("stats = %+v, want nil for a backend that sends no stats", got.Stats)
	}
	if got.Notice.Code != contracts.CodeSettingInvalid || len(got.Notice.Args) != 2 {
		t.Fatalf("notice = %+v, want setting-invalid with two arguments", got.Notice)
	}
	if got.Notice.Args[0].Name() != "Setting" || got.Notice.Args[0].Value() != "compactAt" {
		t.Fatalf("first argument = %s %v, want Setting compactAt", got.Notice.Args[0].Name(), got.Notice.Args[0].Value())
	}
	if got.Notice.Args[1].Name() != "Count" || got.Notice.Args[1].Value() != 3 {
		t.Fatalf("second argument = %s %v, want Count 3", got.Notice.Args[1].Name(), got.Notice.Args[1].Value())
	}
	if got.Note != "compacté" || got.TokensAfter != 12_000 || !got.TokensMeasured {
		t.Fatalf("result = %+v, want the note, the tokens after and the measured flag kept", got)
	}
}

func TestResultWithoutStatsWritesTheOldWireForm(t *testing.T) {
	notice := contracts.Notice{Code: contracts.CodeSettingInvalid, Args: []contracts.Arg{
		contracts.Text("Setting", "compactAt"),
		contracts.Count(3),
	}}
	const want = `{"code":7,"args":[{"name":"Setting","kind":1,"value":"compactAt"},{"name":"Count","kind":2,"value":3}]}`
	if got := mustResult(t, contracts.CompactResult{Notice: notice}).Value; got != want {
		t.Fatalf("Result value = %q, want %q", got, want)
	}
}

func TestResultWithStatsWritesTheOldWireForm(t *testing.T) {
	notice := contracts.Notice{Code: contracts.CodeSettingInvalid, Args: []contracts.Arg{
		contracts.Text("Setting", "compactAt"),
		contracts.Count(3),
	}}
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
	const want = `{"code":7,"args":[{"name":"Setting","kind":1,"value":"compactAt"},{"name":"Count","kind":2,"value":3}],"stats":{"Stage":"backend","Outcome":"short","Calls":4,"Kept":1,"Truncated":2,"Pruned":1,"Pinned":3,"CharsBefore":9000,"CharsAfter":2000,"TokensRemoved":1800,"Requests":2,"RequestTokens":5000,"Duration":1500000000}}`
	if got := mustResult(t, contracts.CompactResult{Notice: notice, Stats: &stats}).Value; got != want {
		t.Fatalf("Result value = %q, want %q", got, want)
	}
}

func TestDecodeResultReadsTheOldWireFormWithStats(t *testing.T) {
	value := `{"code":7,"args":[{"name":"Setting","kind":1,"value":"compactAt"},{"name":"Count","kind":2,"value":3}],"stats":{"Stage":"backend","Outcome":"short","Calls":4,"Kept":1,"Truncated":2,"Pruned":1,"Pinned":3,"CharsBefore":9000,"CharsAfter":2000,"TokensRemoved":1800,"Requests":2,"RequestTokens":5000,"Duration":1500000000}}`
	got, err := DecodeResult(contracts.Event{T: EventResult, Value: value, Text: "compacté", Tokens: 12_000, Done: true})
	if err != nil {
		t.Fatalf("DecodeResult: %v", err)
	}
	if got.Stats == nil || got.Stats.Stage != "backend" || got.Stats.Outcome != contracts.CompactShort || got.Stats.Duration != 1500*time.Millisecond || got.Stats.RequestTokens != 5_000 {
		t.Fatalf("stats = %+v, want the backend stats of the old form", got.Stats)
	}
	if got.Notice.Code != contracts.CodeSettingInvalid || len(got.Notice.Args) != 2 || got.Notice.Args[1].Value() != 3 {
		t.Fatalf("notice = %+v, want setting-invalid with its arguments", got.Notice)
	}
}

func TestUnknownArgKindIsKeptAndReencodedCompact(t *testing.T) {
	value := `{"code":1,"args":[{"name":"X","kind":9,"value":"a"}]}`
	got, err := DecodeResult(contracts.Event{T: EventResult, Value: value})
	if err != nil {
		t.Fatalf("DecodeResult: %v", err)
	}
	encoded, err := EncodeNotice(got.Notice)
	if err != nil {
		t.Fatalf("EncodeNotice: %v", err)
	}
	if encoded != value {
		t.Fatalf("re-encoded notice = %q, want %q", encoded, value)
	}
}

func TestEncodeNoticeRefusesMoreArgumentsThanTheLimit(t *testing.T) {
	args := make([]contracts.Arg, 0, 17)
	for range 17 {
		args = append(args, contracts.Count(3))
	}
	_, err := EncodeNotice(contracts.Notice{Code: contracts.CodeSettingInvalid, Args: args})
	if !errors.Is(err, contracts.ErrNoticeLimit) {
		t.Fatalf("EncodeNotice with 17 arguments: %v, want ErrNoticeLimit", err)
	}
}

func TestResultRefusesStatsWithAnUnknownOutcome(t *testing.T) {
	_, err := Result(contracts.CompactResult{Stats: &contracts.CompactStats{Stage: "jev"}})
	if !errors.Is(err, contracts.ErrUnknownCompactOutcome) {
		t.Fatalf("Result with a zero outcome: %v, want ErrUnknownCompactOutcome", err)
	}
}

func TestDecodeRefusesMoreArgumentsThanTheLimit(t *testing.T) {
	args := strings.TrimSuffix(strings.Repeat(`{"name":"Count","kind":2,"value":3},`, 17), ",")
	requireNoticeMalformed(t, `{"code":1,"args":[`+args+`]}`, contracts.ErrNoticeLimit)
}

func TestDecodeRefusesANameLongerThanTheLimit(t *testing.T) {
	name := strings.Repeat("N", 257)
	requireNoticeMalformed(t, `{"code":1,"args":[{"name":"`+name+`","kind":1,"value":"a"}]}`, contracts.ErrNoticeLimit)
}

func TestDecodeRefusesAValueLongerThanTheLimit(t *testing.T) {
	text := strings.Repeat("a", 70<<10)
	requireNoticeMalformed(t, `{"code":1,"args":[{"name":"X","kind":1,"value":"`+text+`"}]}`, contracts.ErrNoticeLimit)
}

func TestDecodeRefusesAReferenceWithoutValue(t *testing.T) {
	requireNoticeMalformed(t, `{"code":1,"args":[{"name":"R","kind":3}]}`, contracts.ErrNoticeArgs)
}

func TestDecodeRefusesACountWithoutValue(t *testing.T) {
	requireNoticeMalformed(t, `{"code":1,"args":[{"name":"C","kind":2}]}`, contracts.ErrNoticeArgs)
}

func TestDecodeNoticeWrapsTheMalformedErrorWithErrNoticeMalformed(t *testing.T) {
	if _, err := DecodeNotice(`{`); !errors.Is(err, ErrNoticeMalformed) {
		t.Fatalf("DecodeNotice error = %v, want ErrNoticeMalformed", err)
	}
}

func TestLongTextIsBoundedOnTheWire(t *testing.T) {
	notice := contracts.Notice{Code: contracts.CodeHealthUnreachable, Args: []contracts.Arg{contracts.Text("Reason", strings.Repeat("x", 70<<10))}}
	value, err := EncodeNotice(notice)
	if err != nil {
		t.Fatalf("EncodeNotice of a 70 KiB text: %v", err)
	}
	got, err := DecodeNotice(value)
	if err != nil {
		t.Fatalf("DecodeNotice: %v", err)
	}
	reason, isText := got.Args[0].Value().(string)
	if !isText || len(reason) != maxTextBytes+len(truncationMark) || !strings.HasSuffix(reason, truncationMark) {
		t.Fatalf("reason = %d bytes, want %d bytes ending with %q", len(reason), maxTextBytes+len(truncationMark), truncationMark)
	}
}

func TestBoundedTextStaysValidUTF8(t *testing.T) {
	notice := contracts.Notice{Code: contracts.CodeHealthUnreachable, Args: []contracts.Arg{contracts.Text("Reason", "a"+strings.Repeat("é", 3000))}}
	value, err := EncodeNotice(notice)
	if err != nil {
		t.Fatalf("EncodeNotice: %v", err)
	}
	got, err := DecodeNotice(value)
	if err != nil {
		t.Fatalf("DecodeNotice: %v", err)
	}
	reason, isText := got.Args[0].Value().(string)
	if !isText || !utf8.ValidString(reason) {
		t.Fatalf("bounded reason is not valid UTF-8: %q", reason)
	}
}

func requireNoticeMalformed(t *testing.T, value string, cause error) {
	t.Helper()
	_, err := DecodeResult(contracts.Event{T: EventResult, Value: value})
	if !errors.Is(err, ErrNoticeMalformed) || !errors.Is(err, cause) {
		t.Fatalf("DecodeResult error = %v, want ErrNoticeMalformed wrapping %v", err, cause)
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
