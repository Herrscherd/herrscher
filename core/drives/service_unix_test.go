//go:build !windows

package drives_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
		service: drives.NewService(catalog, drives.NewRclone(fake.Command, catalog.RcloneConfig())),
		agent:   drives.Caller{Session: "s1", Agent: "karamon", Worktree: worktree},
	}
}

func (r rig) journal(t *testing.T) []drives.Entry {
	t.Helper()
	entries, err := r.service.Log(drives.Filter{}, 0)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	return entries
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
	if err := os.WriteFile(filepath.Join(r.agent.Worktree, "out.md"), []byte("report"), 0o600); err != nil {
		t.Fatal(err)
	}

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
	for _, source := range []string{"../secret", filepath.Join(outside, "secret"), "escape/secret"} {
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

func TestAReadCopiesIntoTheWorktreeAndRefusesBinaryInline(t *testing.T) {
	r := newRig(t, drives.Drive{Name: "gdrive", Kind: drives.KindGoogleDrive, Default: drives.AccessRead})
	r.fake.Put(t, "gdrive", "img.png", "\x89PNG\xff\xfe")

	if _, err := r.service.Read(t.Context(), r.agent, "gdrive", "img.png"); !errors.Is(err, drives.ErrNotText) {
		t.Fatalf("inline read of binary: err = %v, want ErrNotText", err)
	}
	size, err := r.service.ReadTo(t.Context(), r.agent, "gdrive", "img.png", "assets/img.png")
	if err != nil || size != 6 {
		t.Fatalf("ReadTo = %d, %v", size, err)
	}
	if raw, err := os.ReadFile(filepath.Join(r.agent.Worktree, "assets", "img.png")); err != nil || string(raw) != "\x89PNG\xff\xfe" {
		t.Fatalf("copied file = %q, %v", raw, err)
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

func TestAReadPastTheInlineLimitIsRefused(t *testing.T) {
	r := newRig(t, drives.Drive{Name: "gdrive", Kind: drives.KindGoogleDrive, Default: drives.AccessRead})
	r.fake.Put(t, "gdrive", "big.txt", strings.Repeat("a", drives.InlineReadBytes+1))

	if _, err := r.service.Read(t.Context(), r.agent, "gdrive", "big.txt"); err == nil {
		t.Fatal("a file past the inline limit was returned")
	} else if _, tooLarge := errors.AsType[*drives.TooLargeError](err); !tooLarge {
		t.Fatalf("err = %v, want a TooLargeError", err)
	}
}
