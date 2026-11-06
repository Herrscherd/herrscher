package main

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/service"
)

func TestReportErrorPrintsTheFrenchErrorLine(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	t.Setenv("HERRSCHER_LANG", "fr")
	code := reportError(errors.New("boom"))
	os.Stderr = old
	if closeErr := w.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	out, readErr := io.ReadAll(r)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if code != 1 || strings.TrimSpace(string(out)) != "herrscher : boom" {
		t.Fatalf("code %d, stderr %q; want 1 and %q", code, out, "herrscher : boom")
	}
}

func TestDefaultConfigRendersTheRealGatewaySettingsInFrench(t *testing.T) {
	t.Setenv("HERRSCHER_LANG", "fr")
	cfg, err := service.DefaultConfig()
	if err != nil {
		t.Fatalf("DefaultConfig() with the real plugins: %v", err)
	}
	for _, v := range cfg.EnvVars {
		if v.Key == "DISCORD_BOT_TOKEN" {
			if v.Help != "jeton du bot Discord" {
				t.Fatalf("DISCORD_BOT_TOKEN help = %q, want the French text", v.Help)
			}
			return
		}
	}
	t.Fatal("DISCORD_BOT_TOKEN missing from the default config")
}
