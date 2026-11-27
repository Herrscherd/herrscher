package drives

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestAKeyDriveNeverEntersTheRcloneConfigAndReachesRcloneByItsEnvironment(t *testing.T) {
	catalog := Open(filepath.Join(t.TempDir(), "drives"))
	drive := Drive{Name: "my-r2", Kind: KindR2}
	remote := KeyRemote{Kind: KindR2, AccessKeyID: "AKID", Secret: "s3cr/t+key=", Endpoint: R2Endpoint("acct")}
	if err := catalog.AddKeyDrive(t.Context(), drive, remote); err != nil {
		t.Fatalf("AddKeyDrive: %v", err)
	}
	if _, err := os.Stat(catalog.RcloneConfig()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the rclone configuration was written: %v", err)
	}
	stored, err := catalog.Find("my-r2")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(catalog.Dir(), catalogFile))
	if err != nil || strings.Contains(string(raw), "s3cr/t") {
		t.Fatalf("drives.json holds the secret or is unreadable: %v", err)
	}
	info, err := os.Stat(filepath.Join(catalog.Dir(), secretsFile))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("secrets file mode = %v, %v, want 0600", info, err)
	}
	env, err := catalog.Environment(stored)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"RCLONE_CONFIG_MY-R2_TYPE=s3", "RCLONE_CONFIG_MY-R2_PROVIDER=Cloudflare", "RCLONE_CONFIG_MY-R2_ENDPOINT=https://acct.r2.cloudflarestorage.com", "RCLONE_CONFIG_MY-R2_ACCESS_KEY_ID=AKID", "RCLONE_CONFIG_MY-R2_SECRET_ACCESS_KEY=s3cr/t+key="} {
		if !slices.Contains(env, want) {
			t.Errorf("environment %v lacks %q", env, want)
		}
	}
	if againErr := catalog.AddKeyDrive(t.Context(), drive, remote); !errors.Is(againErr, ErrDriveExists) {
		t.Fatalf("a second drive of the same name: err = %v, want ErrDriveExists", againErr)
	}
	if removeErr := catalog.Remove(t.Context(), "my-r2"); removeErr != nil {
		t.Fatal(removeErr)
	}
	secrets, err := catalog.loadSecrets()
	if err != nil || len(secrets) != 0 {
		t.Fatalf("secrets after remove = %v, %v, want none", secrets, err)
	}
}

func TestAKeyDriveRefusesAValueRcloneWouldMisread(t *testing.T) {
	catalog := Open(filepath.Join(t.TempDir(), "drives"))
	for _, secret := range []string{"", "has space", "x\ny", "x\x00y"} {
		err := catalog.AddKeyDrive(t.Context(), Drive{Name: "s3", Kind: KindS3}, KeyRemote{Kind: KindS3, AccessKeyID: "AKID", Secret: secret})
		if _, bad := errors.AsType[*BadSettingError](err); !bad {
			t.Errorf("secret %q: err = %v, want a BadSettingError", secret, err)
		}
	}
	if err := catalog.AddKeyDrive(t.Context(), Drive{Name: "g", Kind: KindGoogleDrive}, KeyRemote{Kind: KindGoogleDrive}); !errors.Is(err, ErrNotKeyKind) {
		t.Fatalf("an OAuth kind: err = %v, want ErrNotKeyKind", err)
	}
}

func TestTheJournalKeepsItsGenerationsAndReadsAcrossThem(t *testing.T) {
	journal := Open(t.TempDir()).Journal()
	padding := strings.Repeat("x", journalRotateBytes/2)
	for index := range 5 {
		entry := Entry{Time: time.Now(), Drive: "gdrive", Session: "s1", Op: OpRead, Outcome: OutcomeDone, Reason: padding}
		if index == 4 {
			entry.Session = "s2"
		}
		if err := journal.Append(t.Context(), entry); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(journal.generation(2)); err != nil {
		t.Fatalf("no second generation: %v", err)
	}
	all, err := journal.Read(Filter{}, 0)
	if err != nil || len(all.Entries) != 5 {
		t.Fatalf("Read = %d entries, %v, want 5", len(all.Entries), err)
	}
	mine, err := journal.Read(Filter{Session: "s2"}, 0)
	if err != nil || len(mine.Entries) != 1 {
		t.Fatalf("Read of s2 = %+v, %v", mine, err)
	}
	last, err := journal.Read(Filter{}, 1)
	if err != nil || len(last.Entries) != 1 || last.Entries[0].Session != "s2" {
		t.Fatalf("Read limit 1 = %+v, %v, want the newest", last, err)
	}
}

func TestTheJournalCountsTheLinesItCannotRead(t *testing.T) {
	journal := Open(t.TempDir()).Journal()
	if err := journal.Append(t.Context(), Entry{Time: time.Now(), Drive: "gdrive", Op: OpRead, Outcome: OutcomeDone}); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(journal.path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, writeErr := file.WriteString("{torn\nnot json\n"); writeErr != nil {
		t.Fatal(writeErr)
	}
	if closeErr := file.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	log, err := journal.Read(Filter{}, 0)
	if err != nil || len(log.Entries) != 1 || log.Unreadable != 2 {
		t.Fatalf("Read = %+v, %v, want one entry and two unreadable lines", log, err)
	}
}

func TestASessionKeptRefusedCannotFlushTheJournal(t *testing.T) {
	journal := Open(t.TempDir()).Journal()
	start := time.Now()
	for index := range RefusalsPerWindow + 15 {
		entry := Entry{Time: start.Add(time.Duration(index) * time.Millisecond), Session: "noisy", Drive: "gdrive", Op: OpWrite, Outcome: OutcomeRefused}
		if err := journal.Append(t.Context(), entry); err != nil {
			t.Fatal(err)
		}
	}
	if err := journal.Append(t.Context(), Entry{Time: start, Session: "quiet", Drive: "gdrive", Op: OpWrite, Outcome: OutcomeRefused}); err != nil {
		t.Fatal(err)
	}
	if err := journal.Append(t.Context(), Entry{Time: start.Add(RefusalWindow + time.Second), Session: "noisy", Drive: "gdrive", Op: OpWrite, Outcome: OutcomeRefused}); err != nil {
		t.Fatal(err)
	}
	noisy, err := journal.Read(Filter{Session: "noisy"}, 0)
	if err != nil || len(noisy.Entries) != RefusalsPerWindow+1 {
		t.Fatalf("noisy entries = %d, %v, want %d", len(noisy.Entries), err, RefusalsPerWindow+1)
	}
	if last := noisy.Entries[len(noisy.Entries)-1]; last.Suppressed != 15 {
		t.Fatalf("the first refusal of the next window says %d left out, want 15", last.Suppressed)
	}
	quiet, err := journal.Read(Filter{Session: "quiet"}, 0)
	if err != nil || len(quiet.Entries) != 1 {
		t.Fatalf("another session's refusal was held back: %+v, %v", quiet, err)
	}
}
