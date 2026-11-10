package host

import (
	"testing"
	"time"
)

func TestLimitsTimeoutReadsTheEnvironment(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  time.Duration
	}{
		{name: "unset keeps the default", want: 0},
		{name: "a duration is used", value: "45s", want: 45 * time.Second},
		{name: "a malformed value keeps the default", value: "soon", want: 0},
		{name: "a negative value keeps the default", value: "-5s", want: 0},
		{name: "zero keeps the default", value: "0", want: 0},
		{name: "under a second keeps the default", value: "1ns", want: 0},
		{name: "over five minutes keeps the default", value: "100000h", want: 0},
		{name: "just under a second keeps the default", value: "999ms", want: 0},
		{name: "the lower bound is accepted", value: "1s", want: time.Second},
		{name: "the upper bound is accepted", value: "5m", want: 5 * time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(key string) string {
				if key == EnvLimitsTimeout {
					return tt.value
				}
				return ""
			}
			if got := limitsTimeout(getenv); got != tt.want {
				t.Fatalf("limitsTimeout(%q) = %s, want %s", tt.value, got, tt.want)
			}
		})
	}
}
