package agent

import (
	"fmt"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func describeArgs(args []contracts.Arg) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		out = append(out, fmt.Sprintf("%s/%d=%v", a.Name(), a.Kind(), a.Value()))
	}
	return out
}

func TestAgentNoticesCarryTheirCodeArgsAndEnglishText(t *testing.T) {
	cases := []struct {
		name     string
		err      noticed
		text     string
		wantCode contracts.Code
		wantArgs []string
	}{
		{
			name:     "exists",
			err:      &ExistsError{Name: "revue"},
			text:     `agent "revue" already exists`,
			wantCode: contracts.CodeAgentExists,
			wantArgs: []string{"Name/1=revue"},
		},
		{
			name:     "invalid name",
			err:      &InvalidNameError{Name: "Re vue"},
			text:     `invalid agent name "Re vue"`,
			wantCode: contracts.CodeAgentInvalidName,
			wantArgs: []string{"Name/1=Re vue"},
		},
		{
			name:     "not found",
			err:      &NotFoundError{Name: "revue"},
			text:     `no agent "revue"`,
			wantCode: contracts.CodeAgentNotFound,
			wantArgs: []string{"Name/1=revue"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.Error(); got != tc.text {
				t.Fatalf("Error() = %q, want %q", got, tc.text)
			}
			notice := tc.err.Notice()
			if notice.Code != tc.wantCode {
				t.Fatalf("code = %s, want %s", notice.Code.Key(), tc.wantCode.Key())
			}
			if got := describeArgs(notice.Args); fmt.Sprint(got) != fmt.Sprint(tc.wantArgs) {
				t.Fatalf("args = %v, want %v", got, tc.wantArgs)
			}
		})
	}
}

type noticed interface {
	error
	Notice() contracts.Notice
}
