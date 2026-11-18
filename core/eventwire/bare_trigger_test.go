package eventwire

import (
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestCompactionPayloadsRefuseAnEmptyOrUnknownTrigger(t *testing.T) {
	for _, value := range []string{"", "   ", "bogus"} {
		if _, err := DecodeCompactionDone(value); err == nil {
			t.Fatalf("DecodeCompactionDone(%q) = nil error, want a refusal", value)
		}
		if _, err := DecodeCompactionFailure(value); err == nil {
			t.Fatalf("DecodeCompactionFailure(%q) = nil error, want a refusal", value)
		}
	}
}

func TestCompactionPayloadsAcceptTheTrimmedJSONForm(t *testing.T) {
	value := "  {\"trigger\":\"manual\"}\n"
	done, err := DecodeCompactionDone(value)
	if err != nil || done.Trigger != contracts.TriggerManual.String() {
		t.Fatalf("DecodeCompactionDone(%q) = %+v, %v, want the manual trigger", value, done, err)
	}
	failure, err := DecodeCompactionFailure(value)
	if err != nil || failure.Trigger != contracts.TriggerManual.String() {
		t.Fatalf("DecodeCompactionFailure(%q) = %+v, %v, want the manual trigger", value, failure, err)
	}
}

func TestCompactionPayloadsAcceptABareTriggerWithSpaces(t *testing.T) {
	value := "  automatic \n"
	done, err := DecodeCompactionDone(value)
	if err != nil || done.Trigger != contracts.TriggerAutomatic.String() || done.Notice != nil {
		t.Fatalf("DecodeCompactionDone(%q) = %+v, %v, want the automatic trigger", value, done, err)
	}
	failure, err := DecodeCompactionFailure(value)
	if err != nil || failure.Trigger != contracts.TriggerAutomatic.String() || failure.Notice != nil || failure.Reason != "" {
		t.Fatalf("DecodeCompactionFailure(%q) = %+v, %v, want the automatic trigger alone", value, failure, err)
	}
}
