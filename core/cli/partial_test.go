package cli_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/Herrscherd/herrscher/core/cli"
)

type partialFailureError string

func (f partialFailureError) Error() string         { return "command failed" }
func (f partialFailureError) PartialOutput() string { return string(f) }

func TestPartialOutputOfReadsTheOutputThroughWrapping(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{name: "a partial failure yields its output", err: partialFailureError("partial answer"), want: "partial answer"},
		{name: "a wrapped partial failure yields its output", err: fmt.Errorf("dispatch: %w", partialFailureError("partial answer")), want: "partial answer"},
		{name: "a plain failure yields nothing", err: errors.New("refused"), want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := cli.PartialOutputOf(tc.err); got != tc.want {
				t.Fatalf("PartialOutputOf() = %q, want %q", got, tc.want)
			}
		})
	}
}
