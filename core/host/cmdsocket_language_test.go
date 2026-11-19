//go:build !windows

package host

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
)

type languageDispatcher struct {
	seen i18n.Language
}

func (d *languageDispatcher) Dispatch(ctx context.Context, _ []string) (string, error) {
	d.seen = clitext.CallerLanguage(ctx, func() i18n.Language { return i18n.English })
	return "", nil
}

func TestCommandSocketHandsTheClientLanguageToTheVerb(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cmd.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	disp := &languageDispatcher{}
	serveTestCommandSocket(ctx, t, isolatedRuntimeDir(t), path, disp, defaultCommandReadTimeout)
	sendCommand(t, path, cmdRequest{Argv: []string{"session", "scrollback", "api"}, Lang: "fr"})
	if disp.seen != i18n.French {
		t.Fatalf("verb saw %v, want the client language French", disp.seen)
	}
	sendCommand(t, path, cmdRequest{Argv: []string{"session", "scrollback", "api"}})
	if disp.seen != i18n.English {
		t.Fatalf("a request without a language saw %v, want the fallback English", disp.seen)
	}
}

func TestDaemonDispatchSendsTheClientConfigLanguage(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("HERRSCHER_LANG", "")
	t.Setenv("HERRSCHER_STATE_DIR", stateDir)
	if err := os.WriteFile(filepath.Join(stateDir, "config.json"), []byte(`{"language":"fr"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "cmd.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	disp := &languageDispatcher{}
	layout := isolatedRuntimeDir(t)
	serveTestCommandSocket(ctx, t, layout, path, disp, defaultCommandReadTimeout)
	if _, _, err := dispatchLiveCommand(ctx, newHostMessages(), layout, path, []string{"session", "scrollback", "api"}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if disp.seen != i18n.French {
		t.Fatalf("daemon verb saw %v, want the client config language French", disp.seen)
	}
}

type registryDispatcher struct {
	reg *cli.Registry
}

func (d registryDispatcher) Dispatch(ctx context.Context, argv []string) (string, error) {
	return cli.Plain(d.reg.Dispatch(ctx, argv))
}

func TestSocketRequestInFrenchGetsTheRealScrollbackVerbInFrench(t *testing.T) {
	partDir := interruptedTurnTranscript(t)
	reg := cli.NewRegistry(clitext.NewMessages())
	if err := reg.Add(sessionScrollbackVerb(partDir, fixedLanguageSource(i18n.English))); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "cmd.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serveTestCommandSocket(ctx, t, isolatedRuntimeDir(t), path, registryDispatcher{reg: reg}, defaultCommandReadTimeout)

	resp := sendCommand(t, path, cmdRequest{Argv: []string{"session", "scrollback", "--name", "main"}, Lang: "fr"})
	if resp.Err != nil {
		t.Fatalf("session scrollback over the socket failed: %s", *resp.Err)
	}
	if resp.Ok == nil {
		t.Fatal("session scrollback over the socket returned no output")
	}
	var lines []contracts.ScrollbackLine
	if err := json.Unmarshal([]byte(*resp.Ok), &lines); err != nil {
		t.Fatalf("session scrollback JSON: %v", err)
	}
	want, err := contracts.NewRenderer().Render(i18n.French, contracts.Notice{Code: contracts.CodeTurnInterrupted})
	if err != nil {
		t.Fatalf("contracts render: %v", err)
	}
	if len(lines) != 2 || lines[1].Text != want {
		t.Fatalf("scrollback over the socket = %s, want the interruption in French %q", *resp.Ok, want)
	}
}
