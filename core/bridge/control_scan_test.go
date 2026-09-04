package bridge

import (
	"context"
	"errors"
	"io"
	"testing"
)

func TestReportControlScan(t *testing.T) {
	dirty := errors.New("control: malformed event line")
	cases := []struct {
		name    string
		in      error
		wantErr error
	}{
		{name: "clean close", in: nil},
		{name: "eof", in: io.EOF},
		{name: "cancelled", in: context.Canceled},
		{name: "malformed line", in: dirty, wantErr: dirty},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := reportControlScan(tc.in)
			if tc.wantErr == nil {
				if got != nil {
					t.Fatalf("want nil, got %v", got)
				}
				return
			}
			if !errors.Is(got, tc.wantErr) {
				t.Fatalf("want wrapped %v, got %v", tc.wantErr, got)
			}
		})
	}
}
