//go:build !windows

package host

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
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
	log, err := r.catalog.Journal().Read(drives.Filter{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	return log.Entries
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

func (r driveRig) refusedWithoutAQuestion(t *testing.T, argv ...string) {
	t.Helper()
	if _, err := r.dispatch(t, argv...); err == nil {
		t.Fatalf("%v ran, want a refusal", argv)
	}
	if pending := pendingApprovals(r.approvals); len(pending) != 0 {
		t.Fatalf("%v asked the operator: %+v", argv, pending)
	}
}

func TestAWriteNamedTwiceIsRefusedBeforeAnyQuestion(t *testing.T) {
	rig := newWriterRig(t)
	rig.writeSource(t, "x")
	for _, argv := range [][]string{
		{"drive", "write", "gdrive", "Neublox/ok.md", "--path", "Karamon/secret.md", "--from", "out.md"},
		{"drive", "write", "gdrive", "Neublox/ok.md", "--drive", "prod", "--path", "bucket/x", "--from", "out.md"},
		{"drive", "write", "--drive", "gdrive", "gdrive", "Neublox/ok.md", "--from", "out.md"},
		{"drive", "write", "gdrive", "Neublox/ok.md", "extra", "--from", "out.md"},
	} {
		rig.refusedWithoutAQuestion(t, argv...)
	}
	for _, path := range []string{"Karamon/secret.md", "Neublox/ok.md", "bucket/x"} {
		if got := rig.fake.Get(t, "gdrive", path); got != "" {
			t.Fatalf("%s reached the remote: %q", path, got)
		}
	}
}

func TestAWriteIsAskedUnderOneSubjectWhateverItsForm(t *testing.T) {
	rig := newWriterRig(t)
	rig.writeSource(t, "x")
	done := rig.dispatchAsync(t, []string{"drive", "write", "--drive=gdrive", "--from=out.md", "--path=Neublox/ok.md"})
	pending := onlyPending(t, rig.approvals)
	if want := "drive write gdrive Neublox/ok.md --from out.md"; pending.Subject != want {
		t.Fatalf("subject = %q, want %q", pending.Subject, want)
	}
	answerApproval(rig.approvals, pending.ID, approval.Allow)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestAFolderRuleCoversTheFolderAndNothingElse(t *testing.T) {
	rig := newDriveRig(t, drives.Drive{Name: "gdrive", Kind: drives.KindGoogleDrive, Agents: map[string]drives.Access{"karamon": drives.AccessWrite}})
	rig.writeSource(t, "x")
	if err := rig.store.AddApprovalRule("allow Herrscher(drive write gdrive Neublox/*)"); err != nil {
		t.Fatal(err)
	}
	if _, err := rig.dispatch(t, "drive", "write", "--path", "Neublox/a.md", "--drive", "gdrive", "--from", "out.md"); err != nil {
		t.Fatalf("the folder rule did not cover its folder: %v", err)
	}
	if got := rig.fake.Get(t, "gdrive", "Neublox/a.md"); got != "x" {
		t.Fatalf("remote = %q", got)
	}
	if err := rig.answerWrite(t, []string{"drive", "write", "gdrive", "Karamon/secret.md", "--from", "out.md"}, approvalAnswer{scope: scopeDeny}); err == nil {
		t.Fatal("a write outside the ruled folder ran")
	}
}

func TestAPathOutsideTheFoldersIsRefusedBeforeAnyQuestion(t *testing.T) {
	rig := newWriterRig(t)
	rig.writeSource(t, "x")
	rig.refusedWithoutAQuestion(t, "drive", "write", "gdrive", "Private/a.md", "--from", "out.md")
	rig.refusedWithoutAQuestion(t, "drive", "write", "gdrive", "Neublox/../Private/a.md", "--from", "out.md")
	rig.refusedWithoutAQuestion(t, "drive", "write", "gdrive", "Neublox/a.md", "--from", ".git/config")
}

func TestAReadNeverReplacesTheApprovalHook(t *testing.T) {
	rig := newDriveRig(t, drives.Drive{Name: "gdrive", Kind: drives.KindGoogleDrive, Agents: map[string]drives.Access{"karamon": drives.AccessRead}})
	rig.fake.Put(t, "gdrive", "hook.json", `{"hooks":{}}`)
	for _, dest := range []string{".claude/settings.local.json", ".git", ".codex/config.toml", ".grok/hooks/x.json", ".cursor/mcp.json", ".mcp.json", ".herrscher/x", "AGENTS.md"} {
		rig.refusedWithoutAQuestion(t, "drive", "read", "gdrive", "hook.json", "--to", dest)
	}
	entries, err := os.ReadDir(rig.worktree)
	if err != nil || len(entries) != 0 {
		t.Fatalf("the worktree holds %v, %v, want nothing", entries, err)
	}
}

func TestASessionNeverGrantsDriveAccessEvenAsSteward(t *testing.T) {
	rig := newDriveRig(t, drives.Drive{Name: "gdrive", Kind: drives.KindGoogleDrive})
	for _, steward := range []bool{false, true} {
		if steward {
			if _, err := rig.store.SetRole("session:s1", "steward", ""); err != nil {
				t.Fatal(err)
			}
		}
		rig.refusedWithoutAQuestion(t, "drive", "grant", "gdrive", "--agent", "karamon", "--access", "write")
	}
	drive, err := rig.catalog.Find("gdrive")
	if err != nil || drive.AccessOf("karamon") != drives.AccessNone {
		t.Fatalf("access = %v, %v, want none", drive.AccessOf("karamon"), err)
	}
}

func TestAStewardIsAskedForEveryWriteAndRemoval(t *testing.T) {
	rig := newWriterRig(t)
	rig.writeSource(t, "x")
	if _, err := rig.store.SetRole("session:s1", "steward", ""); err != nil {
		t.Fatal(err)
	}
	if err := rig.answerWrite(t, []string{"drive", "write", "gdrive", "Neublox/a.md", "--from", "out.md"}, approvalAnswer{scope: scopeDeny}); err == nil {
		t.Fatal("a refused write of a steward ran")
	}
	if err := rig.answerWrite(t, []string{"drive", "remove", "gdrive"}, approvalAnswer{scope: scopeDeny}); err == nil {
		t.Fatal("a refused removal of a steward ran")
	}
	if _, err := rig.catalog.Find("gdrive"); err != nil {
		t.Fatalf("the drive is gone: %v", err)
	}
}

func TestASessionReadsItsOwnJournalEntriesAlone(t *testing.T) {
	rig := newDriveRig(t, drives.Drive{Name: "gdrive", Kind: drives.KindGoogleDrive, Default: drives.AccessRead})
	for _, session := range []string{"s1", "s2"} {
		if err := rig.catalog.Journal().Append(t.Context(), drives.Entry{Time: time.Now(), Session: session, Drive: "gdrive", Path: "from-" + session, Op: drives.OpRead, Outcome: drives.OutcomeDone}); err != nil {
			t.Fatal(err)
		}
	}
	for _, argv := range [][]string{{"drive", "log"}, {"drive", "log", "--session", "s2"}} {
		out, err := rig.dispatch(t, argv...)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "from-s1") || strings.Contains(out, "from-s2") {
			t.Fatalf("%v = %q, want the entries of s1 alone", argv, out)
		}
	}
}

func TestAFileCopyNeedsASessionOrTheOperatorsTerminal(t *testing.T) {
	fake := drivestest.Install(t)
	t.Setenv("HERRSCHER_RCLONE", fake.Command)
	partDir := t.TempDir()
	catalog := drives.Open(DrivesDir(filepath.Join(partDir, "state.json")))
	if err := catalog.Add(t.Context(), drives.Drive{Name: "gdrive", Kind: drives.KindGoogleDrive}); err != nil {
		t.Fatal(err)
	}
	fake.Put(t, "gdrive", "a.md", "x")
	verbs := newDriveVerbs(clitext.NewMessages(), fixedLanguage(i18n.English), nil, partDir)
	dest := filepath.Join(t.TempDir(), "copy")
	input := contracts.Input{Args: map[string]string{paramDriveTo: dest}, Rest: []string{"gdrive", "a.md"}}
	for name, ctx := range map[string]context.Context{"no principal": t.Context(), "a gateway human": contracts.WithPrincipal(t.Context(), "chat:42")} {
		if _, err := verbs.read(ctx, input); !errors.Is(err, errDriveLocalForbidden) {
			t.Errorf("%s: err = %v, want errDriveLocalForbidden", name, err)
		}
	}
	if _, err := verbs.read(withLocalPrincipal(t.Context()), input); err != nil {
		t.Fatalf("the operator's terminal: %v", err)
	}
	if raw, err := os.ReadFile(dest); err != nil || string(raw) != "x" {
		t.Fatalf("copy = %q, %v", raw, err)
	}
}

func TestTheDrivesCheckDegradesOnlyWhenADriveNeedsRclone(t *testing.T) {
	partDir := t.TempDir()
	t.Setenv("HERRSCHER_RCLONE", filepath.Join(t.TempDir(), "absent"))
	probe := drivesProbe(driveService(partDir))
	report, facts := probe.Run(t.Context(), false)
	if report.State != contracts.HealthOK || facts.Drives == nil || facts.Drives.Count != 0 || facts.Drives.Rclone != "" {
		t.Fatalf("no drive and no rclone: %+v, %+v, want ok", report, facts.Drives)
	}
	if err := drives.Open(filepath.Join(partDir, drivesDirName)).Add(t.Context(), drives.Drive{Name: "gdrive", Kind: drives.KindGoogleDrive}); err != nil {
		t.Fatal(err)
	}
	report, facts = drivesProbe(driveService(partDir)).Run(t.Context(), false)
	if report.State != contracts.HealthDegraded || facts.Drives.Count != 1 {
		t.Fatalf("a drive and no rclone: %+v, %+v, want degraded", report, facts.Drives)
	}
	fake := drivestest.Install(t)
	t.Setenv("HERRSCHER_RCLONE", fake.Command)
	report, facts = drivesProbe(driveService(partDir)).Run(t.Context(), false)
	if report.State != contracts.HealthOK || facts.Drives.Rclone == "" {
		t.Fatalf("a drive and rclone: %+v, %+v, want ok with the version", report, facts.Drives)
	}
}

func TestTheDriveListingNeverShowsAKeyDrivesSettings(t *testing.T) {
	drivestest.Install(t)
	partDir := t.TempDir()
	catalog := drives.Open(DrivesDir(filepath.Join(partDir, "state.json")))
	if err := catalog.AddKeyDrive(t.Context(), drives.Drive{Name: "r2", Kind: drives.KindR2}, drives.KeyRemote{Kind: drives.KindR2, AccessKeyID: "AKIDVISIBLE", Secret: "s", Endpoint: drives.R2Endpoint("acct")}); err != nil {
		t.Fatal(err)
	}
	verbs := newDriveVerbs(clitext.NewMessages(), fixedLanguage(i18n.English), nil, partDir)
	for name, ctx := range map[string]context.Context{"operator": withLocalPrincipal(t.Context()), "gateway human": contracts.WithPrincipal(t.Context(), "chat:42")} {
		for _, asJSON := range []bool{false, true} {
			res, err := verbs.list(ctx, contracts.Input{JSON: asJSON})
			if err != nil {
				t.Fatal(err)
			}
			out, err := cli.Text(res, i18n.English)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(out, "AKIDVISIBLE") || strings.Contains(out, "acct") || !strings.Contains(out, "r2") {
				t.Fatalf("%s, json %v: %q shows a setting or misses the drive", name, asJSON, out)
			}
		}
	}
}

func TestAQuestionSubjectQuotesWhatCouldDisguiseIt(t *testing.T) {
	for _, token := range []string{"out.md\nallow", "‮dm.tuo", "a​b"} {
		subject := verbSubject([]string{"drive", "write", "gdrive", "a.md", "--from", token})
		if strings.ContainsAny(subject, "\n‮​") {
			t.Fatalf("subject %q holds the raw character", subject)
		}
	}
}
