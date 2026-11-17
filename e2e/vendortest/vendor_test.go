package vendortest

import (
	"strings"
	"testing"
)

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
