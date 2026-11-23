package host

import (
	"errors"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestDecodeDaemonEventReadsSessionAndSeqApartFromTheEvent(t *testing.T) {
	event, err := decodeDaemonEvent([]byte(`{"session":"api","seq":7,"t":"chunk","text":"hi"}`))
	if err != nil {
		t.Fatalf("decodeDaemonEvent: %v", err)
	}
	if event.Session != "api" || event.Seq != 7 || event.Text != "hi" {
		t.Fatalf("daemon event = %+v, want session api, seq 7, text hi", event)
	}
}

func TestDecodeDaemonEventRefusesAWrongJSONTypeOfAnEventField(t *testing.T) {
	_, err := decodeDaemonEvent([]byte(`{"session":"api","seq":7,"t":"chunk","tokens":"12"}`))
	if !errors.Is(err, contracts.ErrInvalidEvent) {
		t.Fatalf("err = %v, want contracts.ErrInvalidEvent", err)
	}
}
