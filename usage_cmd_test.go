package main

import (
	"errors"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"

	usagecore "github.com/Herrscherd/herrscher/core/usage"
)

func TestParseUsageArgsSelectsEachView(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    string
		wantErr bool
	}{
		{name: "default is limits", args: nil, want: usageViewLimits},
		{name: "compaction", args: []string{"compaction"}, want: usageViewCompact},
		{name: "cost", args: []string{"cost"}, want: usageViewCost},
		{name: "unknown view is refused", args: []string{"bogus"}, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts, err := parseUsageArgs(i18n.English, tc.args)
			if (err != nil) != tc.wantErr {
				t.Fatalf("parseUsageArgs(%q) error = %v, want error %t", tc.args, err, tc.wantErr)
			}
			if !tc.wantErr && opts.view != tc.want {
				t.Fatalf("view = %q, want %q", opts.view, tc.want)
			}
		})
	}
}

func TestParseUsageArgsRefusesEmptyValues(t *testing.T) {
	for _, args := range [][]string{{"--range="}, {"--account="}, {"--range", ""}, {"--account", ""}} {
		if _, err := parseUsageArgs(i18n.English, args); !errors.Is(err, usagecore.ErrEmptyValue) {
			t.Errorf("parseUsageArgs(%q) error = %v, want ErrEmptyValue", args, err)
		}
	}
}
