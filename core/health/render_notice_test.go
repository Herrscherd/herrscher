package health

import (
	"errors"
	"fmt"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestFallsBackToArgumentsOnlyOnAMismatchedNotice(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"no error renders normally", nil, false},
		{"unknown code falls back", fmt.Errorf("code 9: %w", contracts.ErrUnknownCode), true},
		{"arguments that do not match fall back", fmt.Errorf("argument %q of kind 9: %w", "X", contracts.ErrNoticeArgs), true},
		{"missing catalogue key is kept", fmt.Errorf("code %q: %w", "code.1", contracts.ErrMissingMessage), false},
		{"other errors are kept", errors.New("catalogue unreadable"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := fallsBackToArguments(tc.err); got != tc.want {
				t.Fatalf("fallsBackToArguments(%v) = %t, want %t", tc.err, got, tc.want)
			}
		})
	}
}
