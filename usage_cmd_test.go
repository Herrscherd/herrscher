package main

import (
	"errors"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"

	usagecore "github.com/Herrscherd/herrscher/core/usage"
)

func TestParseUsageArgsRefusesEmptyValues(t *testing.T) {
	for _, args := range [][]string{{"--range="}, {"--account="}, {"--range", ""}, {"--account", ""}} {
		if _, err := parseUsageArgs(i18n.English, args); !errors.Is(err, usagecore.ErrEmptyValue) {
			t.Errorf("parseUsageArgs(%q) error = %v, want ErrEmptyValue", args, err)
		}
	}
}
