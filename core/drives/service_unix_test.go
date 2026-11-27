//go:build !windows

package drives_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/drives"
	"github.com/Herrscherd/herrscher/core/drives/drivestest"
)

type rig struct {
	fake    drivestest.Rclone
	service drives.Service
	agent   drives.Caller
}

func newRig(t *testing.T, drive drives.Drive) rig {
	t.Helper()
	fake := drivestest.Install(t)
	catalog := drives.Open(filepath.Join(t.TempDir(), "drives"))
	if err := catalog.Add(t.Context(), drive); err != nil {
		t.Fatalf("Add: %v", err)
	}
	worktree := t.TempDir()
	return rig{
		fake:    fake,
		service: drives.NewService(catalog, drives.NewRclone(fake.Command, catalog.RcloneConfig())).Protecting([]string{".claude", ".mcp.json", "AGENTS.md"}),
		agent:   drives.Caller{Session: "s1", Agent: "karamon", Worktree: worktree},
	}
}

func (r rig) journal(t *testing.T) []drives.Entry {
	t.Helper()
	log, err := r.service.Log(drives.Filter{}, 0)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	return log.Entries
}

func (r rig) worktreeFile(t *testing.T, name, content string) {
	t.Helper()
	full := filepath.Join(r.agent.Worktree, name)
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAReaderReadsInsideTheFoldersAndTheReadIsJournaled(t *testing.T) {
	r := newRig(t, drives.Drive{Name: "gdrive", Kind: drives.KindGoogleDrive, Folders: []string{"Neublox"}, Agents: map[string]drives.Access{"karamon": drives.AccessRead}})
	r.fake.Put(t, "gdrive", "Neublox/notes.md", "hello")
	r.fake.Put(t, "gdrive", "Private/diary.md", "secret")

	content, err := r.service.Read(t.Context(), r.agent, "gdrive", "Neublox/notes.md")
	if err != nil || content != "hello" {
		t.Fatalf("Read = %q, %v, want hello", content, err)
	}
	if _, err := r.service.Read(t.Context(), r.agent, "gdrive", "Private/diary.md"); !errors.Is(err, drives.ErrOutsideFolders) {
		t.Fatalf("a path outside the folders: err = %v, want ErrOutsideFolders", err)
	}
	entries := r.journal(t)
	if len(entries) != 2 {
		t.Fatalf("journal = %+v, want the read and the refusal", entries)
	}
	if got := entries[0]; got.Session != "s1" || got.Agent != "karamon" || got.Op != drives.OpRead || got.Outcome != drives.OutcomeDone || got.Bytes != 5 || got.Path != "Neublox/notes.md" {
		t.Fatalf("read entry = %+v", got)
	}
	if got := entries[1]; got.Outcome != drives.OutcomeRefused {
		t.Fatalf("refusal entry = %+v", got)
	}
}

func TestAnAgentWithoutAccessIsRefusedAndTheRefusalJournaled(t *testing.T) {
	r := newRig(t, drives.Drive{Name: "gdrive", Kind: drives.KindGoogleDrive})
	r.fake.Put(t, "gdrive", "a.md", "x")

	_, err := r.service.Read(t.Context(), r.agent, "gdrive", "a.md")
	if denied, ok := errors.AsType[*drives.AccessDeniedError](err); !ok || denied.Held != drives.AccessNone || denied.Need != drives.AccessRead {
		t.Fatalf("err = %v, want an AccessDeniedError for read", err)
	}
	if visible, err := r.service.Visible(r.agent); err != nil || len(visible) != 0 {
		t.Fatalf("Visible = %+v, %v, want no drive for an agent with no access", visible, err)
	}
	if entries := r.journal(t); len(entries) != 1 || entries[0].Outcome != drives.OutcomeRefused {
		t.Fatalf("journal = %+v, want one refusal", entries)
	}
	if runs := r.fake.Runs(t); len(runs) != 0 {
		t.Fatalf("rclone ran for a refused read: %q", runs)
	}
}

func TestAWriterSendsAFileOfItsWorktree(t *testing.T) {
	r := newRig(t, drives.Drive{Name: "gdrive", Kind: drives.KindGoogleDrive, Folders: []string{"Neublox"}, Default: drives.AccessWrite})
	r.worktreeFile(t, "out.md", "report")

	size, err := r.service.Write(t.Context(), r.agent, "gdrive", "Neublox/report.md", "out.md")
	if err != nil || size != 6 {
		t.Fatalf("Write = %d, %v", size, err)
	}
	if got := r.fake.Get(t, "gdrive", "Neublox/report.md"); got != "report" {
		t.Fatalf("remote holds %q, want report", got)
	}
}

func TestALocalPathNeverLeavesTheWorktree(t *testing.T) {
	r := newRig(t, drives.Drive{Name: "gdrive", Kind: drives.KindGoogleDrive, Default: drives.AccessWrite})
	r.fake.Put(t, "gdrive", "a.md", "x")
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("s"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(r.agent.Worktree, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(r.agent.Worktree, "link")); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"../secret", filepath.Join(outside, "secret"), "escape/secret", "link"} {
		if _, err := r.service.Write(t.Context(), r.agent, "gdrive", "stolen", source); err == nil {
			t.Errorf("Write from %q succeeded, want a refusal", source)
		}
	}
	for _, dest := range []string{"../copy", "escape/copy"} {
		if _, err := r.service.ReadTo(t.Context(), r.agent, "gdrive", "a.md", dest); err == nil {
			t.Errorf("ReadTo %q succeeded, want a refusal", dest)
		}
	}
	if _, err := os.Stat(filepath.Join(outside, "copy")); err == nil {
		t.Fatal("a copy landed outside the worktree")
	}
	if got := r.fake.Get(t, "gdrive", "stolen"); got != "" {
		t.Fatalf("the remote received %q from outside the worktree", got)
	}
}

func TestAReadNeverLandsInTheFilesThatSteerTheAgent(t *testing.T) {
	r := newRig(t, drives.Drive{Name: "gdrive", Kind: drives.KindGoogleDrive, Default: drives.AccessWrite})
	r.fake.Put(t, "gdrive", "hook.json", `{"hooks":{}}`)
	r.worktreeFile(t, ".claude/settings.local.json", "kept")
	if err := os.Symlink(".claude", filepath.Join(r.agent.Worktree, "settings")); err != nil {
		t.Fatal(err)
	}
	for _, dest := range []string{".claude/settings.local.json", ".git", ".git/config", "sub/.git/config", ".mcp.json", "AGENTS.md", ".Claude/x", "settings/settings.local.json"} {
		if _, err := r.service.ReadTo(t.Context(), r.agent, "gdrive", "hook.json", dest); err == nil {
			t.Errorf("ReadTo %q succeeded, want a refusal", dest)
		}
	}
	for _, source := range []string{".claude/settings.local.json", ".git/config"} {
		if _, err := r.service.Write(t.Context(), r.agent, "gdrive", "leak", source); !errors.Is(err, drives.ErrLocalPathProtected) {
			t.Errorf("Write from %q: err = %v, want ErrLocalPathProtected", source, err)
		}
	}
	if raw, err := os.ReadFile(filepath.Join(r.agent.Worktree, ".claude", "settings.local.json")); err != nil || string(raw) != "kept" {
		t.Fatalf("the hook file changed to %q, %v", raw, err)
	}
	if _, err := os.Stat(filepath.Join(r.agent.Worktree, ".git")); err == nil {
		t.Fatal(".git was created")
	}
}

func TestAFifoNeverBlocksTheDaemon(t *testing.T) {
	r := newRig(t, drives.Drive{Name: "gdrive", Kind: drives.KindGoogleDrive, Default: drives.AccessWrite})
	r.fake.Put(t, "gdrive", "a.md", "content")
	fifo := filepath.Join(r.agent.Worktree, "pipe")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 2)
	go func() {
		_, err := r.service.Write(t.Context(), r.agent, "gdrive", "a.md", "pipe")
		done <- err
	}()
	go func() {
		_, err := r.service.ReadTo(t.Context(), r.agent, "gdrive", "a.md", "pipe")
		done <- err
	}()
	for range 2 {
		select {
		case err := <-done:
			if errors.Is(err, drives.ErrNotRegularFile) {
				continue
			}
			if err != nil {
				t.Fatalf("a FIFO operation failed with %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("an operation on a FIFO blocked")
		}
	}
	info, err := os.Lstat(fifo)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("the copy did not replace the FIFO with the file: %v, %v", info, err)
	}
	if got := r.fake.Get(t, "gdrive", "a.md"); got != "content" {
		t.Fatalf("the remote changed to %q", got)
	}
}

func TestACopyIsPrivateAndLeavesNoStagingFile(t *testing.T) {
	r := newRig(t, drives.Drive{Name: "gdrive", Kind: drives.KindGoogleDrive, Default: drives.AccessRead})
	r.fake.Put(t, "gdrive", "img.png", "\x89PNG\xff\xfe")

	if _, err := r.service.Read(t.Context(), r.agent, "gdrive", "img.png"); !errors.Is(err, drives.ErrNotText) {
		t.Fatalf("inline read of binary: err = %v, want ErrNotText", err)
	}
	size, err := r.service.ReadTo(t.Context(), r.agent, "gdrive", "img.png", "assets/img.png")
	if err != nil || size != 6 {
		t.Fatalf("ReadTo = %d, %v", size, err)
	}
	full := filepath.Join(r.agent.Worktree, "assets", "img.png")
	if raw, readErr := os.ReadFile(full); readErr != nil || string(raw) != "\x89PNG\xff\xfe" {
		t.Fatalf("copied file = %q, %v", raw, readErr)
	}
	for path, want := range map[string]os.FileMode{full: 0o600, filepath.Dir(full): 0o700} {
		if info, statErr := os.Stat(path); statErr != nil || info.Mode().Perm() != want {
			t.Errorf("%s mode = %v, %v, want %v", path, info, statErr, want)
		}
	}
	entries, err := os.ReadDir(filepath.Dir(full))
	if err != nil || len(entries) != 1 {
		t.Fatalf("assets holds %v, %v, want the copy alone", entries, err)
	}
}

func TestAReadOfAFolderIsRefused(t *testing.T) {
	r := newRig(t, drives.Drive{Name: "gdrive", Kind: drives.KindGoogleDrive, Default: drives.AccessRead})
	r.fake.Put(t, "gdrive", "Neublox/a.md", "a")
	r.fake.Put(t, "gdrive", "Neublox/b.md", "b")

	if _, err := r.service.Read(t.Context(), r.agent, "gdrive", "Neublox"); !errors.Is(err, drives.ErrIsFolder) {
		t.Fatalf("inline read of a folder: err = %v, want ErrIsFolder", err)
	}
	if _, err := r.service.ReadTo(t.Context(), r.agent, "gdrive", "Neublox", "all.md"); !errors.Is(err, drives.ErrIsFolder) {
		t.Fatalf("copy of a folder: err = %v, want ErrIsFolder", err)
	}
	if _, err := os.Stat(filepath.Join(r.agent.Worktree, "all.md")); err == nil {
		t.Fatal("a folder was copied")
	}
}

func TestAListingStopsAtItsLimitInsteadOfFailing(t *testing.T) {
	r := newRig(t, drives.Drive{Name: "gdrive", Kind: drives.KindGoogleDrive, Default: drives.AccessRead})
	for index := range drives.ListEntries + 5 {
		r.fake.Put(t, "gdrive", fmt.Sprintf("Big/%05d-%s", index, strings.Repeat("n", 200)), "")
	}

	items, truncated, err := r.service.List(t.Context(), r.agent, "gdrive", "Big")
	if err != nil || !truncated || len(items) != drives.ListEntries {
		t.Fatalf("List = %d items, truncated %v, %v, want %d and truncated", len(items), truncated, err, drives.ListEntries)
	}
}

func TestRcloneRunsOnHerrschersOwnConfigWithoutTheOperatorsVariables(t *testing.T) {
	r := newRig(t, drives.Drive{Name: "gdrive", Kind: drives.KindGoogleDrive, Default: drives.AccessRead})
	t.Setenv("RCLONE_CONFIG", "/home/someone/.config/rclone/rclone.conf")
	t.Setenv("RCLONE_DRIVE_TOKEN", "leak")
	r.fake.Put(t, "gdrive", "Neublox/a.md", "x")

	items, truncated, err := r.service.List(t.Context(), r.agent, "gdrive", "Neublox")
	if err != nil || truncated || len(items) != 1 || items[0].Name != "a.md" {
		t.Fatalf("List = %+v, %v, %v", items, truncated, err)
	}
	if _, err := r.service.Read(t.Context(), r.agent, "gdrive", "Neublox/missing.md"); !errors.Is(err, drives.ErrNoSuchFile) {
		t.Fatalf("a missing file: err = %v, want ErrNoSuchFile", err)
	}
	runs := strings.Join(r.fake.Runs(t), "\n")
	if strings.Contains(runs, "RCLONE_") {
		t.Fatalf("rclone saw the operator's variables:\n%s", runs)
	}
	want := "--config " + r.service.Catalog().RcloneConfig() + " --ask-password=false lsjson --max-depth 1 --no-mimetype -- gdrive:Neublox"
	if !strings.Contains(runs, want) {
		t.Fatalf("runs:\n%s\nwant a line %q", runs, want)
	}
}

func TestAKeyDriveReachesRcloneThroughTheEnvironmentOfItsRunAlone(t *testing.T) {
	fake := drivestest.Install(t)
	catalog := drives.Open(filepath.Join(t.TempDir(), "drives"))
	if err := catalog.AddKeyDrive(t.Context(), drives.Drive{Name: "r2", Kind: drives.KindR2, Default: drives.AccessRead}, drives.KeyRemote{Kind: drives.KindR2, AccessKeyID: "AKID", Secret: "topsecret", Endpoint: drives.R2Endpoint("acct")}); err != nil {
		t.Fatal(err)
	}
	service := drives.NewService(catalog, drives.NewRclone(fake.Command, catalog.RcloneConfig()))
	fake.Put(t, "r2", "bucket/a.md", "x")

	if _, err := service.Read(t.Context(), drives.Caller{}, "r2", "bucket/a.md"); err != nil {
		t.Fatal(err)
	}
	runs := strings.Join(fake.Runs(t), "\n")
	if !strings.Contains(runs, "RCLONE_CONFIG_R2_SECRET_ACCESS_KEY=topsecret") {
		t.Fatalf("rclone did not get the secret in its environment:\n%s", runs)
	}
	for _, line := range fake.Runs(t) {
		if strings.HasPrefix(line, "--config") && strings.Contains(line, "topsecret") {
			t.Fatalf("the secret reached argv: %q", line)
		}
	}
}

func TestARemovalWaitsForTheRunThatMayRefreshAToken(t *testing.T) {
	r := newRig(t, drives.Drive{Name: "gdrive", Kind: drives.KindGoogleDrive, Default: drives.AccessRead})
	r.fake.Put(t, "gdrive", "a.md", "x")
	hold := r.fake.HoldReads(t)
	read := make(chan error, 1)
	go func() {
		_, err := r.service.Read(t.Context(), drives.Caller{}, "gdrive", "a.md")
		read <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for !hold.Started() {
		if time.Now().After(deadline) {
			t.Fatal("the read never started")
		}
		time.Sleep(10 * time.Millisecond)
	}
	removed := make(chan error, 1)
	go func() { removed <- r.service.Rclone().DeleteRemote(t.Context(), "gdrive") }()
	select {
	case err := <-removed:
		t.Fatalf("the removal ran while a read held the configuration: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	hold.Release(t)
	if err := <-read; err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := <-removed; err != nil {
		t.Fatalf("remove: %v", err)
	}
	runs := strings.Join(r.fake.Runs(t), "\n")
	if strings.Index(runs, "end cat") > strings.Index(runs, "config delete gdrive") {
		t.Fatalf("the delete started before the read ended:\n%s", runs)
	}
}

func TestAReadPastTheInlineLimitIsRefused(t *testing.T) {
	r := newRig(t, drives.Drive{Name: "gdrive", Kind: drives.KindGoogleDrive, Default: drives.AccessRead})
	r.fake.Put(t, "gdrive", "big.txt", strings.Repeat("a", drives.InlineReadBytes+1))

	_, err := r.service.Read(t.Context(), r.agent, "gdrive", "big.txt")
	if _, tooLarge := errors.AsType[*drives.TooLargeError](err); !tooLarge {
		t.Fatalf("err = %v, want a TooLargeError", err)
	}
}
