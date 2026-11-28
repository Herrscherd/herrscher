//go:build !windows

package drives

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/drives/drivestest"
)

func journalRig(t *testing.T) (drivestest.Rclone, Service, Caller) {
	t.Helper()
	fake := drivestest.Install(t)
	catalog := Open(filepath.Join(t.TempDir(), "drives"))
	if err := catalog.Add(t.Context(), Drive{Name: "gdrive", Kind: KindGoogleDrive, Default: AccessWrite}); err != nil {
		t.Fatal(err)
	}
	return fake, NewService(catalog, NewRclone(fake.Command, catalog.RcloneConfig())), Caller{Session: "s1", Agent: "karamon", Worktree: t.TempDir()}
}

func TestEveryReadThatRunsIsJournaledWithItsPath(t *testing.T) {
	fake, service, caller := journalRig(t)
	fake.Put(t, "gdrive", "decoy.md", "d")
	fake.Put(t, "gdrive", "secret.md", "s")
	for range RefusalsPerWindow + 1 {
		if _, err := service.Read(t.Context(), caller, "gdrive", "decoy.md"); err != nil {
			t.Fatal(err)
		}
	}
	if content, err := service.Read(t.Context(), caller, "gdrive", "secret.md"); err != nil || content != "s" {
		t.Fatalf("Read = %q, %v", content, err)
	}
	log, err := service.Log(Filter{Session: "s1"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(log.Entries) != RefusalsPerWindow+2 || log.Entries[len(log.Entries)-1].Path != "secret.md" {
		t.Fatalf("journal holds %d entries, last %+v, want every read and secret.md last", len(log.Entries), log.Entries[len(log.Entries)-1])
	}
}

func TestAReadPastTheOperationCapNeverRuns(t *testing.T) {
	fake, service, caller := journalRig(t)
	fake.Put(t, "gdrive", "secret.md", "s")
	now := time.Now().UTC()
	for range OperationsPerWindow {
		if err := service.Catalog().Journal().Reserve(t.Context(), caller.Session, now); err != nil {
			t.Fatal(err)
		}
	}
	_, err := service.Read(t.Context(), caller, "gdrive", "secret.md")
	if _, capped := errors.AsType[*OperationCapError](err); !capped {
		t.Fatalf("err = %v, want an OperationCapError", err)
	}
	if runs := strings.Join(fake.Runs(t), "\n"); strings.Contains(runs, " cat ") || strings.Contains(runs, "--stat") {
		t.Fatalf("rclone ran past the cap:\n%s", runs)
	}
	log, err := service.Log(Filter{Session: "s1"}, 0)
	if err != nil || len(log.Entries) != 1 || log.Entries[0].Outcome != OutcomeRefused {
		t.Fatalf("journal = %+v, %v, want the refusal", log.Entries, err)
	}
}

func TestAnOperationWhoseEntryCannotBeWrittenWithholdsItsResult(t *testing.T) {
	fake, service, caller := journalRig(t)
	fake.Put(t, "gdrive", "secret.md", "s")
	if err := os.MkdirAll(filepath.Join(service.Catalog().Journal().dir, fileKey(caller.Session)+activitySuffix), 0o700); err != nil {
		t.Fatal(err)
	}
	content, err := service.Read(t.Context(), caller, "gdrive", "secret.md")
	if _, unjournaled := errors.AsType[*JournalError](err); !unjournaled || content != "" {
		t.Fatalf("Read = %q, %v, want nothing and a JournalError", content, err)
	}
	if _, err := service.ReadTo(t.Context(), caller, "gdrive", "secret.md", "copy.md"); err == nil {
		t.Fatal("a copy whose entry could not be written succeeded")
	}
	entries, err := os.ReadDir(caller.Worktree)
	if err != nil || len(entries) != 0 {
		t.Fatalf("the worktree holds %v, %v, want nothing", entries, err)
	}
	if _, _, err := service.List(t.Context(), caller, "gdrive", "x"); err == nil {
		t.Fatal("a listing whose entry could not be written succeeded")
	}
}

func TestAnUnsentWriteIsCappedAndNeverFillsTheWritesFile(t *testing.T) {
	_, service, caller := journalRig(t)
	for range RefusalsPerWindow + 10 {
		if _, err := service.Write(t.Context(), caller, "gdrive", "a.md", "missing.md"); err == nil {
			t.Fatal("a write from a missing file succeeded")
		}
	}
	journal := service.Catalog().Journal()
	if _, err := os.Stat(filepath.Join(journal.dir, fileKey(caller.Session)+writesSuffix)); err == nil {
		t.Fatal("an unsent write reached the file of the writes")
	}
	log, err := service.Log(Filter{Session: "s1"}, 0)
	if err != nil || len(log.Entries) != RefusalsPerWindow {
		t.Fatalf("journal holds %d entries, %v, want %d", len(log.Entries), err, RefusalsPerWindow)
	}
	for _, entry := range log.Entries {
		if entry.Outcome != OutcomeUnsent {
			t.Fatalf("entry %+v, want unsent", entry)
		}
	}
}

func TestADoneWriteGoesToTheWritesFileUncapped(t *testing.T) {
	fake, service, caller := journalRig(t)
	if err := os.WriteFile(filepath.Join(caller.Worktree, "out.md"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	for range RefusalsPerWindow + 5 {
		if _, err := service.Write(t.Context(), caller, "gdrive", "a.md", "out.md"); err != nil {
			t.Fatal(err)
		}
	}
	if got := fake.Get(t, "gdrive", "a.md"); got != "x" {
		t.Fatalf("remote = %q", got)
	}
	raw, err := os.ReadFile(filepath.Join(service.Catalog().Journal().dir, fileKey(caller.Session)+writesSuffix))
	if err != nil || strings.Count(string(raw), "\n") != RefusalsPerWindow+5 {
		t.Fatalf("writes file holds %d lines, %v, want every write", strings.Count(string(raw), "\n"), err)
	}
}
