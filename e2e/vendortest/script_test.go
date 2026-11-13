package vendortest

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestParseScriptSplitsTurnsAndEchoesPastTheEnd(t *testing.T) {
	script, err := ParseScript(strings.NewReader("text one\ntool Bash {\"command\":\"ls\"}\n---\n\nerror refused\n"))
	if err != nil {
		t.Fatal(err)
	}
	kinds := func(turn Turn) []stepKind {
		var got []stepKind
		for _, s := range turn.steps {
			got = append(got, s.kind)
		}
		return got
	}
	for index, want := range [][]stepKind{{stepText, stepTool}, {stepFail}, {stepEcho}} {
		if got := kinds(script.Turn(index)); !slices.Equal(got, want) {
			t.Errorf("turn %d: got %v, want %v", index, got, want)
		}
	}
}

func TestParseScriptNamesTheBadLine(t *testing.T) {
	for _, tc := range []struct {
		source string
		line   int
		reason error
	}{
		{"text ok\nsing loudly\n", 2, errUnknownStep},
		{"tool Bash\n", 1, errToolNeedsArg},
		{"tool Bash {nope\n", 1, errToolInput},
		{"\ntext\n", 2, errNeedsText},
	} {
		_, err := ParseScript(strings.NewReader(tc.source))
		lineErr, ok := errors.AsType[*LineError](err)
		if !ok || lineErr.Line != tc.line || !errors.Is(err, tc.reason) {
			t.Errorf("%q: got %v, want line %d: %v", tc.source, err, tc.line, tc.reason)
		}
	}
}
