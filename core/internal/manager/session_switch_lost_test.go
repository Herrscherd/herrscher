package manager

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

type jammedRestartSup struct {
	t          *testing.T
	statePath  string
	restartErr error
	restarted  []state.Session
}

func (s *jammedRestartSup) StartWithin(state.Session, time.Duration) error { return nil }

func (s *jammedRestartSup) StopWithin(string, time.Duration) error { return nil }

func (s *jammedRestartSup) RestartWithin(sess state.Session, _ time.Duration) error {
	s.restarted = append(s.restarted, sess)
	if len(s.restarted) > 1 {
		return nil
	}
	blockStateSaves(s.t, s.statePath)
	return s.restartErr
}

func TestSessionSwitchNamesTheLostSessionWhenRestartAndRollbackFail(t *testing.T) {
	restartErr := errors.New("agent exited")
	cases := []struct {
		name         string
		lang         i18n.Language
		wantError    string
		wantRendered string
	}{
		{
			name:         "english",
			lang:         i18n.English,
			wantError:    "backend restart: %[1]v; rollback failed, session s1 out of service: %[2]v",
			wantRendered: "unexpected error: backend restart: %[1]v; rollback failed, session s1 out of service: %[2]v",
		},
		{
			name:         "french",
			lang:         i18n.French,
			wantError:    "backend restart: %[1]v; rollback failed, session s1 out of service: %[2]v",
			wantRendered: "erreur inattendue : red\u00e9marrage backend : %[1]v ; le retour arrière a échoué, session s1 hors service : %[2]v",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "s.json")
			err := switchWithLostRollback(t, path, tc.lang, restartErr)
			assertLostSession(t, err, restartErr, filepath.Dir(path), tc.lang, tc.wantError, tc.wantRendered)
		})
	}
}

func switchWithLostRollback(t *testing.T, path string, lang i18n.Language, restartErr error) error {
	t.Helper()
	st := state.NewState(path)
	if err := st.AddSession(state.Session{Name: "s1", ChannelID: "42", Type: "text", Vendor: "claude", Cmd: "claude"}); err != nil {
		t.Fatal(err)
	}
	sup := &jammedRestartSup{t: t, statePath: path, restartErr: restartErr}
	h := NewHandler(clitext.NewMessages(), &fakeChannelAdmin{}, sup, &fakeWT{path: t.TempDir(), scratchBase: t.TempDir()}, &fakeForge{}, &fakeUpdater{}, agent.NewStore(t.TempDir()), st, "claude", t.TempDir(), []string{"chat"}, nil, nil)
	h.SetLanguage(lang)
	_, err := h.sessionSwitchRun(context.Background(), args(
		"name", "s1", "vendor", "codex", "cmd", "codex --model gpt-5-codex", "handoff", "none",
	))
	if err == nil {
		t.Fatal("a switch whose restart and rollback both failed reported success")
	}
	if len(sup.restarted) != 2 {
		t.Fatalf("the rollback was not attempted after the failed restart: restarted=%d", len(sup.restarted))
	}
	return err
}

func assertLostSession(t *testing.T, err, restartErr error, faultDir string, lang i18n.Language, wantErrorFormat, wantRenderedFormat string) {
	t.Helper()
	var pathErr *fs.PathError
	if !errors.As(err, &pathErr) {
		t.Fatalf("no *fs.PathError in the chain, the rollback cause is lost: %v", err)
	}
	if filepath.Dir(pathErr.Path) != faultDir {
		t.Fatalf("rollback failed on %q, want a write inside the jammed state directory %q", pathErr.Path, faultDir)
	}
	if !errors.Is(err, restartErr) {
		t.Fatalf("errors.Is(restart cause) = false, the cause is lost: %v", err)
	}
	if got, want := err.Error(), fmt.Sprintf(wantErrorFormat, restartErr, pathErr); got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	if got, want := cli.Localize(clitext.NewMessages(), err, lang).Error(), fmt.Sprintf(wantRenderedFormat, restartErr, pathErr); got != want {
		t.Fatalf("notice = %q, want %q", got, want)
	}
}

func TestRestartLostFallsBackToTheEnglishLineWhenTheCatalogIsBroken(t *testing.T) {
	saved := contracts.Default
	t.Cleanup(func() { contracts.Default = saved })
	contracts.Default = contracts.Registry{}
	contracts.Default.Register(collidingCatalogPlugin())
	messages := clitext.NewMessages()
	if messages.Err() == nil {
		t.Fatal("a plugin declaring a host key did not break the catalog")
	}
	restartErr := errors.New("agent exited")
	rollbackErr := errors.New("state file jammed")
	const want = "backend restart: agent exited; rollback failed, session s1 out of service: state file jammed"
	cases := []struct {
		lang   i18n.Language
		prefix string
	}{
		{lang: i18n.English, prefix: "unexpected error: "},
		{lang: i18n.French, prefix: "erreur inattendue : "},
	}
	for _, tc := range cases {
		err := restartLostError(messages, tc.lang, "s1", restartErr, rollbackErr)
		if got := err.Error(); got != want {
			t.Fatalf("%v: Error() = %q, want %q", tc.lang, got, want)
		}
		if got, wantNotice := cli.Localize(messages, err, tc.lang).Error(), tc.prefix+want; got != wantNotice {
			t.Fatalf("%v: notice = %q, want %q", tc.lang, got, wantNotice)
		}
	}
}

func collidingCatalogPlugin() contracts.Plugin {
	entry := []byte(`[{"id":"cli.session.switch.restart_lost","description":"","other":"rogue"}]`)
	return contracts.Plugin{
		Manifest: contracts.Manifest{Kind: "rogue", Category: contracts.CategorySkills},
		Catalog: fstest.MapFS{
			"en.json": {Data: entry},
			"fr.json": {Data: entry},
		},
	}
}
