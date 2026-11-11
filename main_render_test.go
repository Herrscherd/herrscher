package main

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/clitext"
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
	want := map[string]string{
		"DISCORD_BOT_TOKEN": "jeton du bot Discord",
		"TERMINAL_LEARN":    "la session qu'un lancement ouvre apprend : false rétablit une session qui n'enregistre rien",
	}
	got := make(map[string]string)
	for _, v := range cfg.EnvVars {
		if _, ok := want[v.Key]; ok {
			got[v.Key] = v.Help
		}
	}
	for key, help := range want {
		if got[key] != help {
			t.Fatalf("%s help = %q, want %q", key, got[key], help)
		}
	}
}

func TestRunRefusesACatalogCollisionBeforeAnyCommand(t *testing.T) {
	saved := contracts.Default
	t.Cleanup(func() { contracts.Default = saved })
	contracts.Default = contracts.Registry{}
	contracts.Default.Register(collidingPlugin("alpha", "a"))
	contracts.Default.Register(collidingPlugin("beta", "b"))
	refusal := clitext.For(clitext.ClientLanguage()).Err()
	if refusal == nil {
		t.Fatal("the colliding plugins must break the catalogs")
	}

	code, stdout, stderr := runCaptured(t, "help")
	if code != 1 {
		t.Fatalf("runWith() = %d, want 1 on a catalog collision", code)
	}
	if stdout != "" {
		t.Fatalf("stdout %q: the command was dispatched after the refusal", stdout)
	}
	if want := refusal.Error() + "\n"; stderr != want {
		t.Fatalf("stderr = %q, want exactly the refusal line %q", stderr, want)
	}
}

func runCaptured(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	savedArgs := os.Args
	t.Cleanup(func() { os.Args = savedArgs })
	os.Args = append([]string{"herrscher"}, args...)
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	savedOut, savedErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW
	code := runWith(testRuntime(t))
	os.Stdout, os.Stderr = savedOut, savedErr
	for _, w := range []*os.File{outW, errW} {
		if closeErr := w.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
	}
	stdout, readErr := io.ReadAll(outR)
	if readErr != nil {
		t.Fatal(readErr)
	}
	stderr, readErr := io.ReadAll(errR)
	if readErr != nil {
		t.Fatal(readErr)
	}
	return code, string(stdout), string(stderr)
}

func collidingPlugin(kind, text string) contracts.Plugin {
	return contracts.Plugin{
		Manifest: contracts.Manifest{Kind: kind, Category: contracts.CategoryGateway},
		Catalog: fstest.MapFS{
			"en.json": &fstest.MapFile{Data: []byte(`[{"id":"x.dup","description":"","other":"` + text + `"}]`)},
		},
	}
}
