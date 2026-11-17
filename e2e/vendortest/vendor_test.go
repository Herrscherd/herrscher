package vendortest

import (
	"errors"
	"strings"
	"testing"
)

func TestScriptedRefusesTheStepsItCannotReplay(t *testing.T) {
	cases := []struct {
		name   string
		script string
	}{
		{name: "usage", script: "usage 950\n"},
		{name: "tool", script: "tool Bash {\"command\":\"ls\"}\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			script, err := ParseScript(strings.NewReader(tc.script))
			if err != nil {
				t.Fatal(err)
			}
			_, err = (&vendor{}).scripted(script.Turn(0), "hi")
			if !errors.Is(err, errUnsupportedStep) {
				t.Fatalf("err = %v, want errUnsupportedStep: a step the fake cannot replay must fail, not vanish", err)
			}
		})
	}
}

func TestScriptedKeepsTheTextSpokenBeforeAFailure(t *testing.T) {
	script, err := ParseScript(strings.NewReader("text partial answer\nerror boom\n"))
	if err != nil {
		t.Fatal(err)
	}
	reply, err := (&vendor{}).scripted(script.Turn(0), "hi")
	if err != nil {
		t.Fatal(err)
	}
	if !reply.failed || reply.failure != "boom" || reply.text != "partial answer" {
		t.Fatalf("got %+v, want the text kept with the failure", reply)
	}
}
