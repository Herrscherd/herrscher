package usage

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestRangeRoundTrip(t *testing.T) {
	for _, rng := range Ranges() {
		text, err := rng.MarshalText()
		if err != nil {
			t.Fatalf("MarshalText(%v): %v", rng, err)
		}
		parsed, err := ParseRange(string(text))
		if err != nil || parsed != rng {
			t.Errorf("ParseRange(%q) = %v, %v; want %v", text, parsed, err, rng)
		}
	}
}

func TestParseRangeRefusesUnknown(t *testing.T) {
	if _, err := ParseRange("1y"); !errors.Is(err, ErrUnknownValue) {
		t.Fatalf("ParseRange(1y) error = %v, want ErrUnknownValue", err)
	}
}

func TestLimitsStatusJSON(t *testing.T) {
	raw, err := json.Marshal(LimitsUnavailable)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var back LimitsStatus
	if err := json.Unmarshal(raw, &back); err != nil || back != LimitsUnavailable {
		t.Fatalf("round trip = %v, %v; raw %s", back, err, raw)
	}
	if err := back.UnmarshalText([]byte("bogus")); !errors.Is(err, ErrUnknownValue) {
		t.Fatalf("UnmarshalText(bogus) error = %v, want ErrUnknownValue", err)
	}
}

func TestParseAccountKey(t *testing.T) {
	tests := []struct {
		text string
		want AccountKey
	}{
		{text: "", want: AccountKey{}},
		{text: "claude:native", want: AccountKey{Vendor: "claude"}},
		{text: "codex:work", want: AccountKey{Vendor: "codex", Account: "work"}},
	}
	for _, tt := range tests {
		got, err := ParseAccountKey(tt.text)
		if err != nil || got != tt.want {
			t.Errorf("ParseAccountKey(%q) = %+v, %v; want %+v", tt.text, got, err, tt.want)
		}
		if got.Vendor != "" && got.String() != tt.text {
			t.Errorf("String() = %q, want %q", got.String(), tt.text)
		}
	}
	if _, err := ParseAccountKey("nocolon"); !errors.Is(err, ErrUnknownValue) {
		t.Fatalf("ParseAccountKey(nocolon) error = %v, want ErrUnknownValue", err)
	}
}
