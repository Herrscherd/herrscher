package main

import (
	"errors"
	"io"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
)

func TestParseConfigRejectsBadFlags(t *testing.T) {
	binary := fakeBinary(t)
	withBudget := []string{"-herrscher", binary, "-budget-tokens", "10"}
	cases := []struct {
		name string
		args []string
		key  clitext.Key
	}{
		{"missing budget", []string{"-herrscher", binary}, clitext.MembenchErrBudgetMissing},
		{"negative tokens", []string{"-herrscher", binary, "-budget-tokens", "-5"}, clitext.MembenchErrFlagValue},
		{"unknown setting", append(append([]string{}, withBudget...), "-settings", "bogus"), clitext.MembenchErrFlagValue},
		{"unknown format", append(append([]string{}, withBudget...), "-format", "xml"), clitext.MembenchErrFlagValue},
		{"max turns below three", append(append([]string{}, withBudget...), "-max-turns", "2"), clitext.MembenchErrFlagValue},
		{"missing binary", []string{"-herrscher", "/nonexistent/membench-herrscher", "-budget-tokens", "10"}, clitext.MembenchErrHerrscher},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseConfig(tc.args, io.Discard, clitext.NewMessages(), i18n.English)
			known, ok := errors.AsType[messageError](err)
			if !ok || known.key != tc.key {
				t.Errorf("error %v, want key %s", err, tc.key)
			}
		})
	}
}

func TestParseConfigAcceptsASelectedSetting(t *testing.T) {
	cfg, err := parseConfig([]string{"-herrscher", fakeBinary(t), "-budget-usd", "2", "-settings", "report,raw-archive"}, io.Discard, clitext.NewMessages(), i18n.English)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.settings) != 2 || cfg.settings[0].env != "MEMORY_REPORT_ENABLED" || cfg.settings[1].env != "MEMORY_RAW_ARCHIVE" {
		t.Errorf("settings %+v, want report then raw-archive", cfg.settings)
	}
	if cfg.model != defaultModel || cfg.budget.usd != 2 {
		t.Errorf("model %q, budget %+v; want the default model and a two dollar budget", cfg.model, cfg.budget)
	}
}
