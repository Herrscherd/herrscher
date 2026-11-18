package eventwire

import (
	"fmt"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestCompactionDoneWireFormatIsFrozen(t *testing.T) {
	withArg := contracts.Notice{Code: contracts.CodeCompactUnavailable, Args: []contracts.Arg{contracts.Text("Reason", "bridge closed")}}
	cases := []struct {
		name string
		done CompactionDone
		want string
	}{
		{
			name: "no relayed notice",
			done: CompactionDone{Trigger: "manual"},
			want: `{"trigger":"manual"}`,
		},
		{
			name: "relayed notice with an argument",
			done: CompactionDone{Trigger: "automatic", Notice: &withArg},
			want: fmt.Sprintf(`{"trigger":"automatic","notice":{"code":%d,"args":[{"name":"Reason","kind":%d,"value":"bridge closed"}]}}`, uint16(contracts.CodeCompactUnavailable), uint8(contracts.ArgText)),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.done.Encode()
			if err != nil {
				t.Fatalf("Encode() = %v", err)
			}
			if got != tc.want {
				t.Fatalf("Encode() = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestDecodeCompactionDoneReadsAnOlderHostsBareTrigger(t *testing.T) {
	done, err := DecodeCompactionDone("automatic")
	if err != nil {
		t.Fatalf("DecodeCompactionDone(bare trigger) = %v, want no error", err)
	}
	if done.Trigger != "automatic" || done.Notice != nil {
		t.Fatalf("DecodeCompactionDone(bare trigger) = %+v, want the trigger and no notice", done)
	}
}
