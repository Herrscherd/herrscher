package control

import (
	"errors"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestScanEventsRefusesAWrongJSONTypeOfAnEventField(t *testing.T) {
	handled := 0
	err := ScanEvents(strings.NewReader(`{"t":"chunk","tokens":"12"}`+"\n"), func(contracts.Event) error {
		handled++
		return nil
	})
	if !errors.Is(err, contracts.ErrInvalidEvent) {
		t.Fatalf("err = %v, want contracts.ErrInvalidEvent", err)
	}
	if !strings.Contains(err.Error(), "tokens") {
		t.Fatalf("err = %q, want it to name the wire field tokens", err)
	}
	if handled != 0 {
		t.Fatalf("handled %d events, want the malformed line refused before any handler", handled)
	}
}

func TestScanEventsKeepsAWellFormedEvent(t *testing.T) {
	var got contracts.Event
	err := ScanEvents(strings.NewReader(`{"t":"chunk","text":"hi","tokens":12}`+"\n"), func(event contracts.Event) error {
		got = event
		return nil
	})
	if err != nil {
		t.Fatalf("ScanEvents: %v", err)
	}
	if got.Text != "hi" || got.Tokens != 12 {
		t.Fatalf("event = %+v, want text hi and tokens 12", got)
	}
}
