//go:build !windows

package host

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestTheSameArgvIsRefusedOnOneSocketAndRunOnTheOther(t *testing.T) {
	st, err := state.LoadState(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if addErr := st.AddSession(state.Session{Name: "s1"}); addErr != nil {
		t.Fatalf("AddSession: %v", addErr)
	}
	reg := &cli.Registry{}
	approvalTab := newApprovalTable(io.Discard)
	sessionTab := newSessionTable()
	if approveErr := addApproveCommands(reg, fixedLanguage(i18n.English), io.Discard, st, nil, approvalTab, sessionTab); approveErr != nil {
		t.Fatalf("addApproveCommands: %v", approveErr)
	}
	h := &hub{warnings: io.Discard, st: st, reg: reg, live: make(map[string]liveSession)}

	dir := t.TempDir()
	operatorSock := filepath.Join(dir, "op.sock")
	sessionSock := filepath.Join(dir, "s1.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serveCommandSocket(ctx, isolatedRuntimeDir(t), operatorSock, asOperator{disp: h, lang: h.textLanguage()}, testLanguage(), io.Discard)
	serveCommandSocket(ctx, isolatedRuntimeDir(t), sessionSock, asSession{disp: h, session: "s1", lang: h.sessionLanguageSource("s1")}, testLanguage(), io.Discard)

	argv := []string{"approve", "rule", "allow Bash(*)"}

	out, _, err := dispatchLiveCommand(ctx, isolatedRuntimeDir(t), operatorSock, argv)
	if err != nil {
		t.Fatalf("the operator socket was refused: %v", err)
	}
	if out == "" {
		t.Fatal("the operator's call produced nothing")
	}

	_, _, err = dispatchLiveCommand(ctx, isolatedRuntimeDir(t), sessionSock, argv)
	if err == nil {
		t.Fatal("the same argv on the session socket must be refused")
	}
	if !strings.Contains(err.Error(), "approve rule") {
		t.Fatalf("refusal %q must name the verb", err)
	}

	if got := len(st.ApprovalRules()); got != 1 {
		t.Fatalf("%d rules stored, want 1", got)
	}
}

func TestServedOperatorSocketFollowsTheLiveLanguage(t *testing.T) {
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	live := newOperatorLanguage(i18n.English)
	reg := &cli.Registry{}
	if err := addScheduleCommands(reg, live.Get, st, fakeAgents{known: make(map[string]bool)}, &schedulerSlot{}); err != nil {
		t.Fatalf("addScheduleCommands: %v", err)
	}
	h := &hub{warnings: io.Discard, st: st, reg: reg, lang: i18n.English, language: live.Get, live: make(map[string]liveSession)}
	layout := isolatedRuntimeDir(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if _, err := serveHubSockets(ctx, layout, "served", h); err != nil {
		t.Fatalf("serveHubSockets: %v", err)
	}
	live.set(i18n.French)
	_, _, err := dispatchLiveCommand(ctx, layout, CommandSocketPathIn(layout, "served"), []string{"schedule", "rm", "--name", "fantome"})
	if err == nil {
		t.Fatal("schedule rm of an unknown schedule succeeded")
	}
	if want := notFoundScheduleText(i18n.French, "fantome"); err.Error() != want {
		t.Fatalf("the served operator socket replied %q, want the french %q", err.Error(), want)
	}
}
