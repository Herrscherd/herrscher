package drives

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
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
	if _, err := os.Stat(generation(journal.path, 2)); err != nil {
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
	for index := range EntriesPerWindow + 15 {
		entry := Entry{Time: start.Add(time.Duration(index) * time.Millisecond), Session: "noisy", Drive: "gdrive", Op: OpWrite, Outcome: OutcomeRefused}
		if err := journal.Append(t.Context(), entry); err != nil {
			t.Fatal(err)
		}
	}
	if err := journal.Append(t.Context(), Entry{Time: start, Session: "quiet", Drive: "gdrive", Op: OpWrite, Outcome: OutcomeRefused}); err != nil {
		t.Fatal(err)
	}
	if err := journal.Append(t.Context(), Entry{Time: start.Add(EntryWindow + time.Second), Session: "noisy", Drive: "gdrive", Op: OpWrite, Outcome: OutcomeRefused}); err != nil {
		t.Fatal(err)
	}
	noisy, err := journal.Read(Filter{Session: "noisy"}, 0)
	if err != nil || len(noisy.Entries) != EntriesPerWindow+1 {
		t.Fatalf("noisy entries = %d, %v, want %d", len(noisy.Entries), err, EntriesPerWindow+1)
	}
	if last := noisy.Entries[len(noisy.Entries)-1]; last.Suppressed != 15 {
		t.Fatalf("the first refusal of the next window says %d left out, want 15", last.Suppressed)
	}
	quiet, err := journal.Read(Filter{Session: "quiet"}, 0)
	if err != nil || len(quiet.Entries) != 1 {
		t.Fatalf("another session's refusal was held back: %+v, %v", quiet, err)
	}
}

func TestNoListingOrReadPushesAWriteOutOfTheJournal(t *testing.T) {
	journal := Open(t.TempDir()).Journal()
	start := time.Now()
	if err := journal.Append(t.Context(), Entry{Time: start, Session: "s1", Drive: "gdrive", Path: "Neublox/report.md", Op: OpWrite, Outcome: OutcomeDone}); err != nil {
		t.Fatal(err)
	}
	padding := strings.Repeat("x", journalRotateBytes/2)
	for index := range 2 * (JournalGenerations + 2) {
		entry := Entry{Time: start.Add(time.Duration(index+1) * time.Second), Drive: "gdrive", Op: OpRead, Outcome: OutcomeDone, Reason: padding}
		if err := journal.Append(t.Context(), entry); err != nil {
			t.Fatal(err)
		}
	}
	log, err := journal.Read(Filter{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if first := log.Entries[0]; first.Op != OpWrite || first.Path != "Neublox/report.md" {
		t.Fatalf("the oldest entry is %+v, want the write that every read rotated past", first)
	}
}

func TestASessionsListingsAndReadsAreCountedLikeItsRefusals(t *testing.T) {
	journal := Open(t.TempDir()).Journal()
	start := time.Now()
	for index := range EntriesPerWindow + 10 {
		if err := journal.Append(t.Context(), Entry{Time: start.Add(time.Duration(index) * time.Millisecond), Session: "s1", Drive: "gdrive", Op: OpList, Outcome: OutcomeDone}); err != nil {
			t.Fatal(err)
		}
	}
	for range 3 {
		if err := journal.Append(t.Context(), Entry{Time: start, Session: "s1", Drive: "gdrive", Op: OpWrite, Outcome: OutcomeDone}); err != nil {
			t.Fatal(err)
		}
	}
	log, err := journal.Read(Filter{Session: "s1"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	listings, writes := 0, 0
	for _, entry := range log.Entries {
		switch entry.Op {
		case OpList:
			listings++
		case OpWrite:
			writes++
		case OpRead:
		}
	}
	if listings != EntriesPerWindow || writes != 3 {
		t.Fatalf("listings = %d, writes = %d, want %d and every write", listings, writes, EntriesPerWindow)
	}
}

func TestTheSessionRatesForgetAnEndedWindow(t *testing.T) {
	journal := Open(t.TempDir()).Journal()
	start := time.Now()
	for _, entry := range []Entry{
		{Time: start, Session: "gone", Drive: "gdrive", Op: OpList, Outcome: OutcomeDone},
		{Time: start.Add(EntryWindow + time.Second), Session: "later", Drive: "gdrive", Op: OpList, Outcome: OutcomeDone},
	} {
		if err := journal.Append(t.Context(), entry); err != nil {
			t.Fatal(err)
		}
	}
	rates, err := journal.loadRates()
	if err != nil {
		t.Fatal(err)
	}
	if _, kept := rates["gone"]; kept || len(rates) != 1 {
		t.Fatalf("rates = %v, want the ended window of gone dropped", rates)
	}
}

func TestALimitedReadStopsBeforeTheOlderFiles(t *testing.T) {
	journal := Open(t.TempDir()).Journal()
	if err := os.WriteFile(generation(journal.path, 1), []byte("not json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	for index := range 3 {
		if err := journal.Append(t.Context(), Entry{Time: start.Add(time.Duration(index) * time.Second), Drive: "gdrive", Path: strconv.Itoa(index), Op: OpRead, Outcome: OutcomeDone}); err != nil {
			t.Fatal(err)
		}
	}
	log, err := journal.Read(Filter{}, 2)
	if err != nil || len(log.Entries) != 2 || log.Entries[0].Path != "1" || log.Entries[1].Path != "2" || log.Unreadable != 0 {
		t.Fatalf("Read limit 2 = %+v, %v, want the two newest and no older file read", log, err)
	}
}

func TestARemovalNeverLeavesADriveWithoutItsSecret(t *testing.T) {
	catalog := Open(filepath.Join(t.TempDir(), "drives"))
	remote := KeyRemote{Kind: KindS3, AccessKeyID: "AKID", Secret: "secret"}
	if err := catalog.AddKeyDrive(t.Context(), Drive{Name: "s3", Kind: KindS3}, remote); err != nil {
		t.Fatal(err)
	}
	secrets := filepath.Join(catalog.Dir(), secretsFile)
	if err := os.Remove(secrets); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(secrets, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := catalog.Remove(t.Context(), "s3"); err == nil {
		t.Fatal("the removal reported no failure to forget the secret")
	}
	if _, err := catalog.Find("s3"); err == nil {
		t.Fatal("the drive outlived a removal whose secret could not be forgotten")
	}
	if err := catalog.AddKeyDrive(t.Context(), Drive{Name: "b", Kind: KindS3}, remote); err == nil {
		t.Fatal("a drive was added although its secret could not be written")
	}
	if all, err := catalog.Load(); err != nil || len(all) != 0 {
		t.Fatalf("drives = %+v, %v, want none: a drive without its secret was recorded", all, err)
	}
}

func TestAnOrphanSecretIsReplacedByTheNextAdd(t *testing.T) {
	catalog := Open(filepath.Join(t.TempDir(), "drives"))
	if err := catalog.Ensure(); err != nil {
		t.Fatal(err)
	}
	if err := catalog.saveSecrets(map[string]string{"s3": "stale"}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.AddKeyDrive(t.Context(), Drive{Name: "s3", Kind: KindS3}, KeyRemote{Kind: KindS3, AccessKeyID: "AKID", Secret: "fresh"}); err != nil {
		t.Fatal(err)
	}
	drive, err := catalog.Find("s3")
	if err != nil {
		t.Fatal(err)
	}
	env, err := catalog.Environment(drive)
	if err != nil || !slices.Contains(env, "RCLONE_CONFIG_S3_SECRET_ACCESS_KEY=fresh") {
		t.Fatalf("environment = %v, %v, want the fresh secret", env, err)
	}
}
