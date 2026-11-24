package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
)

func fakeBinary(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "herrscher")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func noEnv(string) string { return "" }

func TestParseConfigRejectsBadFlags(t *testing.T) {
	binary := fakeBinary(t)
	withBudget := []string{"-herrscher", binary, "-budget-tokens", "10"}
	cases := []struct {
		name string
		args []string
		key  msgKey
	}{
		{"missing budget", []string{"-herrscher", binary}, msgErrBudgetMissing},
		{"negative tokens", []string{"-herrscher", binary, "-budget-tokens", "-5"}, msgErrFlagValue},
		{"unknown setting", append(append([]string{}, withBudget...), "-settings", "bogus"), msgErrFlagValue},
		{"unknown format", append(append([]string{}, withBudget...), "-format", "xml"), msgErrFlagValue},
		{"max turns below three", append(append([]string{}, withBudget...), "-max-turns", "2"), msgErrFlagValue},
		{"missing binary", []string{"-herrscher", "/nonexistent/membench-herrscher", "-budget-tokens", "10"}, msgErrHerrscher},
		{"api key variable unset", append(append([]string{}, withBudget...), "-api-key-env", "MEMBENCH_ABSENT"), msgErrFlagValue},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseConfig(tc.args, io.Discard, loadCatalogue(), i18n.English, noEnv)
			known, ok := errors.AsType[messageError](err)
			if !ok || known.key != tc.key {
				t.Errorf("error %v, want key %s", err, tc.key)
			}
		})
	}
}

func TestParseConfigReadsTheAPIKeyFromTheNamedVariable(t *testing.T) {
	const secret = "placeholder-key"
	getenv := func(name string) string {
		if name == "MEMBENCH_KEY" {
			return secret
		}
		return ""
	}
	cfg, err := parseConfig([]string{"-herrscher", fakeBinary(t), "-budget-tokens", "10", "-api-key-env", "MEMBENCH_KEY"}, io.Discard, loadCatalogue(), i18n.English, getenv)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.apiKey != secret {
		t.Errorf("api key %q, want the value of the named variable", cfg.apiKey)
	}
}

func TestParseConfigAcceptsASelectedSetting(t *testing.T) {
	cfg, err := parseConfig([]string{"-herrscher", fakeBinary(t), "-budget-usd", "2", "-settings", "report,raw-archive"}, io.Discard, loadCatalogue(), i18n.English, noEnv)
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
