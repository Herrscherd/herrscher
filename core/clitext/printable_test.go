package clitext

import "testing"

func TestPrintableEscapesTextParamsAndKeepsTheRest(t *testing.T) {
	got := Printable(Params{"Name": "a\x1b[31m", "Count": 3})
	if got["Name"] != `a\x1b[31m` {
		t.Errorf("Name = %q, want the escaped text", got["Name"])
	}
	if got["Count"] != 3 {
		t.Errorf("Count = %v, want 3 untouched", got["Count"])
	}
}

func TestPrintableOfNilIsNil(t *testing.T) {
	if Printable(nil) != nil {
		t.Error("Printable(nil) is not nil")
	}
}
