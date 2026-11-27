package manager

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

type jammedRestartSup struct {
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
	if err := os.Mkdir(s.statePath+".tmp", 0o700); err != nil {
		return err
	}
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
			wantError:    "backend restart: agent exited; rollback failed, session s1 out of service: ",
			wantRendered: "unexpected error: backend restart: agent exited; rollback failed, session s1 out of service: ",
		},
		{
			name:         "french",
			lang:         i18n.French,
			wantError:    "backend restart: agent exited; rollback failed, session s1 out of service: ",
			wantRendered: "erreur inattendue : red\u00e9marrage backend : agent exited ; le retour arrière a échoué, session s1 hors service : ",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := switchWithLostRollback(t, tc.lang, restartErr)
			assertLostSession(t, err, restartErr, tc.lang, tc.wantError, tc.wantRendered)
		})
	}
}

func switchWithLostRollback(t *testing.T, lang i18n.Language, restartErr error) error {
	t.Helper()
	path := filepath.Join(t.TempDir(), "s.json")
	st := state.NewState(path)
	if err := st.AddSession(state.Session{Name: "s1", ChannelID: "42", Type: "text", Vendor: "claude", Cmd: "claude"}); err != nil {
		t.Fatal(err)
	}
	sup := &jammedRestartSup{statePath: path, restartErr: restartErr}
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

func assertLostSession(t *testing.T, err, restartErr error, lang i18n.Language, wantError, wantRendered string) {
	t.Helper()
	if !errors.Is(err, restartErr) {
		t.Fatalf("errors.Is(restart cause) = false, the cause is lost: %v", err)
	}
	if got := err.Error(); !strings.HasPrefix(got, wantError) || got == wantError {
		t.Fatalf("Error() = %q, want the English notice naming session s1 and both causes after %q", got, wantError)
	}
	if got := renderNotice(t, lang, err); !strings.HasPrefix(got, wantRendered) || got == wantRendered {
		t.Fatalf("notice = %q, want the %v notice naming session s1 and both causes after %q", got, lang, wantRendered)
	}
}
