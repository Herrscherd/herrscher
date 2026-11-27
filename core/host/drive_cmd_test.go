//go:build !windows

package host

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/drives"
	"github.com/Herrscherd/herrscher/core/drives/drivestest"
	"github.com/Herrscherd/herrscher/core/internal/approval"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

type driveRig struct {
	escalationRig

	fake     drivestest.Rclone
	catalog  drives.Catalog
	worktree string
}

func newDriveRig(t *testing.T, drive drives.Drive) driveRig {
	t.Helper()
	fake := drivestest.Install(t)
	t.Setenv("HERRSCHER_RCLONE", fake.Command)
	partDir := t.TempDir()
	st, err := state.LoadState(filepath.Join(partDir, "state.json"))
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	worktree := t.TempDir()
	if err := st.AddSession(state.Session{Name: "s1", Agent: "karamon", Worktree: worktree}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	catalog := drives.Open(DrivesDir(filepath.Join(partDir, "state.json")))
	if err := catalog.Add(t.Context(), drive); err != nil {
		t.Fatalf("Add: %v", err)
	}
	reg := cli.NewRegistry(clitext.NewMessages())
	approvalTab := newApprovalTable(io.Discard).withRules(st)
	sessionTab := newSessionTable(newHostMessages(), clitext.NewMessages())
	if err := addApproveCommands(reg, newHostMessages(), clitext.NewMessages(), fixedLanguage(i18n.English), io.Discard, st, nil, approvalTab, sessionTab); err != nil {
		t.Fatalf("addApproveCommands: %v", err)
	}
	if err := addVerbs(reg, newDriveVerbs(clitext.NewMessages(), fixedLanguage(i18n.English), st, partDir).commands()...); err != nil {
		t.Fatal(err)
	}
	desk := newApprovalDesk(newHostMessages(), fixedLanguage(i18n.English), io.Discard, st, nil, approvalTab, sessionTab)
	events := &eventLog{}
	h := testHub(t, &hub{messages: newHostMessages(), cliMessages: clitext.NewMessages(), warnings: io.Discard, st: st, reg: reg, desk: desk, partDir: partDir, live: make(map[string]liveSession), eventPublisher: events.publish, selfRestarts: newSelfRestarts(st)})
	socket := filepath.Join(t.TempDir(), "s1.sock")
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	serveTestCommandSocket(ctx, t, isolatedRuntimeDir(t), socket, asSession{disp: h, session: "s1", lang: h.sessionLanguageSource("s1"), messages: clitext.NewMessages()}, defaultCommandReadTimeout)
	return driveRig{
		escalationRig: escalationRig{store: st, approvals: approvalTab, sessions: sessionTab, socket: socket, events: events},
		fake:          fake,
		catalog:       catalog,
		worktree:      worktree,
	}
}

func (r driveRig) journal(t *testing.T) []drives.Entry {
	t.Helper()
	entries, err := r.catalog.Journal().Read(drives.Filter{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func TestASessionReadsADriveItsAgentMayRead(t *testing.T) {
	rig := newDriveRig(t, drives.Drive{Name: "gdrive", Kind: drives.KindGoogleDrive, Folders: []string{"Neublox"}, Agents: map[string]drives.Access{"karamon": drives.AccessRead}})
	rig.fake.Put(t, "gdrive", "Neublox/notes.md", "hello from the drive")

	out, err := rig.dispatch(t, "drive", "read", "gdrive", "Neublox/notes.md")
	if err != nil || !strings.Contains(out, "hello from the drive") {
		t.Fatalf("drive read = %q, %v", out, err)
	}
	listed, err := rig.dispatch(t, "drive", "list")
	if err != nil || !strings.Contains(listed, "gdrive") || !strings.Contains(listed, "read") {
		t.Fatalf("drive list = %q, %v", listed, err)
	}
	if pending := pendingApprovals(rig.approvals); len(pending) != 0 {
		t.Fatalf("a read asked the operator: %+v", pending)
	}
	if entries := rig.journal(t); len(entries) != 1 || entries[0].Agent != "karamon" || entries[0].Session != "s1" || entries[0].Op != drives.OpRead {
		t.Fatalf("journal = %+v, want the read of s1 as karamon", entries)
	}
	if notices := rig.events.all(); len(notices) != 0 {
		t.Fatalf("a read was announced: %v", notices)
	}
}

func TestASessionWithoutAccessIsRefusedWithoutAQuestion(t *testing.T) {
	rig := newDriveRig(t, drives.Drive{Name: "gdrive", Kind: drives.KindGoogleDrive, Agents: map[string]drives.Access{"karamon": drives.AccessRead}, Default: drives.AccessNone})
	rig.fake.Put(t, "gdrive", "a.md", "x")
	if err := os.WriteFile(filepath.Join(rig.worktree, "out.md"), []byte("y"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := rig.dispatch(t, "drive", "write", "gdrive", "a.md", "--from", "out.md"); err == nil || !strings.Contains(err.Error(), "write") {
		t.Fatalf("a write without write access: err = %v, want a refusal", err)
	}
	if pending := pendingApprovals(rig.approvals); len(pending) != 0 {
		t.Fatalf("the operator was asked about a write the agent may not make: %+v", pending)
	}
	if got := rig.fake.Get(t, "gdrive", "a.md"); got != "x" {
		t.Fatalf("the remote file changed to %q", got)
	}
	entries := rig.journal(t)
	if len(entries) != 1 || entries[0].Outcome != drives.OutcomeRefused || entries[0].Op != drives.OpWrite {
		t.Fatalf("journal = %+v, want one refused write", entries)
	}
}

func (r driveRig) answerWrite(t *testing.T, argv []string, answer approvalAnswer) error {
	t.Helper()
	done := r.dispatchAsync(t, argv)
	pending := onlyPending(t, r.approvals)
	if pending.Tool != approval.VerbTool || pending.Subject != strings.Join(argv, " ") {
		t.Fatalf("pending = %+v, want the write asked as Herrscher(%s)", pending, strings.Join(argv, " "))
	}
	if !settleApproval(r.approvals, pending.ID, answer) {
		t.Fatal("the question vanished before the operator answered")
	}
	return <-done
}

func (r driveRig) writeSource(t *testing.T, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(r.worktree, "out.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func newWriterRig(t *testing.T) driveRig {
	t.Helper()
	return newDriveRig(t, drives.Drive{Name: "gdrive", Kind: drives.KindGoogleDrive, Folders: []string{"Neublox"}, Agents: map[string]drives.Access{"karamon": drives.AccessWrite}})
}

func TestTheAlwaysRulePassesTheSameWriteWithoutAskingAgain(t *testing.T) {
	rig := newWriterRig(t)
	argv := []string{"drive", "write", "gdrive", "Neublox/report.md", "--from", "out.md"}
	rig.writeSource(t, "first")

	if err := rig.answerWrite(t, argv, approvalAnswer{scope: scopeAlways}); err != nil {
		t.Fatalf("the approved write failed: %v", err)
	}
	if got := rig.fake.Get(t, "gdrive", "Neublox/report.md"); got != "first" {
		t.Fatalf("remote = %q, want first", got)
	}
	rig.writeSource(t, "second")
	if out, err := rig.dispatch(t, argv...); err != nil {
		t.Fatalf("the write the always rule covers failed: %q, %v", out, err)
	}
	if got := rig.fake.Get(t, "gdrive", "Neublox/report.md"); got != "second" {
		t.Fatalf("remote = %q, want second", got)
	}
	if rules := rig.store.ApprovalRules(); len(rules) != 1 || !strings.Contains(rules[0], strings.Join(argv, " ")) {
		t.Fatalf("rules = %v, want the exact write", rules)
	}
	if entries := rig.journal(t); len(entries) != 2 || entries[0].Op != drives.OpWrite || entries[1].Outcome != drives.OutcomeDone {
		t.Fatalf("journal = %+v, want the two writes", entries)
	}
}

func TestAWriteToAnotherFileIsAskedAgainAndARefusalSendsNothing(t *testing.T) {
	rig := newWriterRig(t)
	rig.writeSource(t, "report")
	if err := rig.answerWrite(t, []string{"drive", "write", "gdrive", "Neublox/report.md", "--from", "out.md"}, approvalAnswer{scope: scopeAlways}); err != nil {
		t.Fatalf("the approved write failed: %v", err)
	}

	if err := rig.answerWrite(t, []string{"drive", "write", "gdrive", "Neublox/other.md", "--from", "out.md"}, approvalAnswer{scope: scopeDeny}); err == nil {
		t.Fatal("a refused write ran")
	}
	if got := rig.fake.Get(t, "gdrive", "Neublox/other.md"); got != "" {
		t.Fatalf("the refused write reached the remote: %q", got)
	}
}

func TestNoDriveVerbPrintsTheTokenOfTheRemote(t *testing.T) {
	rig := newDriveRig(t, drives.Drive{Name: "gdrive", Kind: drives.KindGoogleDrive, Default: drives.AccessRead})
	token := `{"access_token":"ya29.leak","refresh_token":"1//leak"}`
	if err := os.WriteFile(rig.catalog.RcloneConfig(), []byte("[gdrive]\ntype = drive\ntoken = "+token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rig.fake.Put(t, "gdrive", "a.md", "x")
	for _, argv := range [][]string{{"drive", "list"}, {"drive", "list", "--json"}, {"drive", "ls", "gdrive"}, {"drive", "read", "gdrive", "a.md"}, {"drive", "log"}} {
		out, err := rig.dispatch(t, argv...)
		if err != nil {
			t.Fatalf("%v: %v", argv, err)
		}
		if strings.Contains(out, "leak") {
			t.Fatalf("%v printed the token: %q", argv, out)
		}
	}
}
