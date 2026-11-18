package eventwire

import (
	"fmt"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestCompactionFailureWireFormatIsFrozen(t *testing.T) {
	withArg := contracts.Notice{Code: contracts.CodeCompactUnavailable, Args: []contracts.Arg{contracts.Text("Reason", "bridge closed")}}
	cases := []struct {
		name    string
		failure CompactionFailure
		want    string
	}{
		{
			name:    "trigger alone",
			failure: CompactionFailure{Trigger: "manual"},
			want:    `{"trigger":"manual"}`,
		},
		{
			name:    "trigger with a reason",
			failure: CompactionFailure{Trigger: "manual", Reason: CompactReasonBridgeGone},
			want:    `{"trigger":"manual","reason":"bridge_gone"}`,
		},
		{
			name:    "trigger with a notice carrying an argument",
			failure: CompactionFailure{Trigger: "automatic", Notice: &withArg},
			want:    fmt.Sprintf(`{"trigger":"automatic","notice":{"code":%d,"args":[{"name":"Reason","kind":%d,"value":"bridge closed"}]}}`, uint16(contracts.CodeCompactUnavailable), uint8(contracts.ArgText)),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.failure.Encode()
			if err != nil {
				t.Fatalf("Encode() = %v", err)
			}
			if got != tc.want {
				t.Fatalf("Encode() = %s, want %s", got, tc.want)
			}
		})
	}
}
