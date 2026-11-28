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
		entry := Entry{Time: time.Now(), Drive: "gdrive", Op: OpRead, Outcome: OutcomeDone, Reason: padding}
		if index == 4 {
			entry.Agent = "a2"
		}
		if err := journal.Append(t.Context(), entry); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(generation(journal.operatorPath(), 2)); err != nil {
		t.Fatalf("no second generation: %v", err)
	}
	all, err := journal.Read(Filter{}, 0)
	if err != nil || len(all.Entries) != 5 {
		t.Fatalf("Read = %d entries, %v, want 5", len(all.Entries), err)
	}
	mine, err := journal.Read(Filter{Agent: "a2"}, 0)
	if err != nil || len(mine.Entries) != 1 {
		t.Fatalf("Read of a2 = %d entries, %v", len(mine.Entries), err)
	}
	last, err := journal.Read(Filter{}, 1)
	if err != nil || len(last.Entries) != 1 || last.Entries[0].Agent != "a2" {
		t.Fatalf("Read limit 1 = %+v, %v, want the newest", last, err)
	}
}

func TestTheJournalCountsTheLinesItCannotRead(t *testing.T) {
	journal := Open(t.TempDir()).Journal()
	if err := journal.Append(t.Context(), Entry{Time: time.Now(), Drive: "gdrive", Op: OpRead, Outcome: OutcomeDone}); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(journal.operatorPath(), os.O_APPEND|os.O_WRONLY, 0o600)
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
	if err := journal.Append(t.Context(), Entry{Time: start.Add(Window + time.Second), Session: "noisy", Drive: "gdrive", Op: OpWrite, Outcome: OutcomeRefused}); err != nil {
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

func TestEveryListingAndReadIsRecordedWhileOnlyEmptyEntriesAreCapped(t *testing.T) {
	journal := Open(t.TempDir()).Journal()
	start := time.Now()
	for index := range RefusalsPerWindow + 10 {
		at := start.Add(time.Duration(index) * time.Millisecond)
		for _, entry := range []Entry{
			{Time: at, Session: "s1", Drive: "gdrive", Path: "p" + strconv.Itoa(index), Op: OpRead, Outcome: OutcomeDone},
			{Time: at, Session: "s1", Drive: "gdrive", Op: OpWrite, Outcome: OutcomeUnsent},
		} {
			if err := journal.Append(t.Context(), entry); err != nil {
				t.Fatal(err)
			}
		}
	}
	log, err := journal.Read(Filter{Session: "s1"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	reads, unsent := 0, 0
	for _, entry := range log.Entries {
		switch entry.Outcome {
		case OutcomeDone:
			reads++
		case OutcomeUnsent:
			unsent++
		case OutcomeRefused, OutcomeFailed:
		}
	}
	if reads != RefusalsPerWindow+10 || unsent != RefusalsPerWindow {
		t.Fatalf("reads = %d, unsent writes = %d, want every read and %d unsent writes", reads, unsent, RefusalsPerWindow)
	}
	if _, err := os.Stat(filepath.Join(journal.dir, fileKey("s1")+writesSuffix)); err == nil {
		t.Fatal("an unsent write went to the file of the writes that reached the remote")
	}
}

func TestASessionOnlyEverRotatesItsOwnHistory(t *testing.T) {
	journal := Open(t.TempDir()).Journal()
	start := time.Now()
	if err := journal.Append(t.Context(), Entry{Time: start, Session: "quiet", Drive: "gdrive", Path: "kept.md", Op: OpRead, Outcome: OutcomeDone}); err != nil {
		t.Fatal(err)
	}
	padding := strings.Repeat("x", journalRotateBytes/2)
	for index := range 2 * (JournalGenerations + 2) {
		if err := journal.Append(t.Context(), Entry{Time: start.Add(time.Duration(index+1) * time.Second), Session: "noisy", Drive: "gdrive", Op: OpRead, Outcome: OutcomeDone, Reason: padding}); err != nil {
			t.Fatal(err)
		}
	}
	quiet, err := journal.Read(Filter{Session: "quiet"}, 0)
	if err != nil || len(quiet.Entries) != 1 || quiet.Entries[0].Path != "kept.md" {
		t.Fatalf("quiet = %+v, %v, want its read kept", quiet, err)
	}
}

func TestASessionPastItsOperationCapIsRefusedBeforeRunning(t *testing.T) {
	journal := Open(t.TempDir()).Journal()
	now := time.Now()
	for range OperationsPerWindow {
		if err := journal.Reserve(t.Context(), "s1", now); err != nil {
			t.Fatal(err)
		}
	}
	if _, capped := errors.AsType[*OperationCapError](journal.Reserve(t.Context(), "s1", now)); !capped {
		t.Fatal("an operation past the cap was reserved")
	}
	if err := journal.Reserve(t.Context(), "s2", now); err != nil {
		t.Fatalf("another session was capped: %v", err)
	}
	if err := journal.Reserve(t.Context(), "", now); err != nil {
		t.Fatalf("the operator was capped: %v", err)
	}
	if err := journal.Reserve(t.Context(), "s1", now.Add(Window)); err != nil {
		t.Fatalf("the next window is still capped: %v", err)
	}
}

func TestTheSessionRatesForgetAnEndedWindow(t *testing.T) {
	journal := Open(t.TempDir()).Journal()
	start := time.Now()
	if err := journal.Reserve(t.Context(), "gone", start); err != nil {
		t.Fatal(err)
	}
	if err := journal.Reserve(t.Context(), "later", start.Add(Window+time.Second)); err != nil {
		t.Fatal(err)
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
	if err := os.MkdirAll(journal.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(generation(journal.operatorPath(), 1), []byte("not json\n"), 0o600); err != nil {
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

func (j Journal) operatorPath() string {
	return filepath.Join(j.dir, operatorFile+activitySuffix)
}

func TestTheJournalDropsTheFilesOfIdleSessionsButKeepsTheOperators(t *testing.T) {
	journal := Open(t.TempDir()).Journal()
	now := time.Now()
	for _, entry := range []Entry{
		{Time: now, Session: "idle", Drive: "gdrive", Op: OpRead, Outcome: OutcomeDone},
		{Time: now, Session: "busy", Drive: "gdrive", Op: OpRead, Outcome: OutcomeDone},
		{Time: now, Drive: "gdrive", Op: OpWrite, Outcome: OutcomeDone},
	} {
		if err := journal.Append(t.Context(), entry); err != nil {
			t.Fatal(err)
		}
	}
	old := now.Add(-JournalRetention - time.Hour)
	for _, name := range []string{fileKey("idle") + activitySuffix, operatorFile + writesSuffix} {
		if err := os.Chtimes(filepath.Join(journal.dir, name), old, old); err != nil {
			t.Fatal(err)
		}
	}
	if err := journal.pruneFiles(now); err != nil {
		t.Fatal(err)
	}
	for name, kept := range map[string]bool{fileKey("idle") + activitySuffix: false, fileKey("busy") + activitySuffix: true, operatorFile + writesSuffix: true} {
		if _, err := os.Stat(filepath.Join(journal.dir, name)); (err == nil) != kept {
			t.Errorf("%s kept = %v, want %v", name, err == nil, kept)
		}
	}
}

func TestTheJournalSizeCapDropsTheOldestFilesAndTheOperatorsWritesLast(t *testing.T) {
	journal := Open(t.TempDir()).Journal()
	if err := os.MkdirAll(journal.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	files := []struct {
		name string
		age  time.Duration
	}{
		{operatorFile + writesSuffix, 10 * time.Hour},
		{fileKey("a") + activitySuffix + ".1", 5 * time.Hour},
		{fileKey("a") + activitySuffix, 4 * time.Hour},
		{fileKey("b") + activitySuffix, time.Hour},
	}
	for _, file := range files {
		path := filepath.Join(journal.dir, file.name)
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Truncate(path, JournalMaxBytes/3); err != nil {
			t.Fatal(err)
		}
		at := now.Add(-file.age)
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
	}
	if err := journal.pruneFiles(now); err != nil {
		t.Fatal(err)
	}
	for index, file := range files {
		_, err := os.Stat(filepath.Join(journal.dir, file.name))
		if kept := err == nil; kept != (index != 1) {
			t.Errorf("%s kept = %v", file.name, kept)
		}
	}
}

func TestAnUnfilteredReadLooksAtTheMostRecentFilesAlone(t *testing.T) {
	journal := Open(t.TempDir()).Journal()
	now := time.Now()
	for index := range JournalReadFiles + 5 {
		session := "s" + strconv.Itoa(index)
		if err := journal.Append(t.Context(), Entry{Time: now, Session: session, Drive: "gdrive", Path: session, Op: OpRead, Outcome: OutcomeDone}); err != nil {
			t.Fatal(err)
		}
		at := now.Add(time.Duration(index-JournalReadFiles-5) * time.Hour)
		if err := os.Chtimes(filepath.Join(journal.dir, fileKey(session)+activitySuffix), at, at); err != nil {
			t.Fatal(err)
		}
	}
	log, err := journal.Read(Filter{}, 0)
	if err != nil || len(log.Entries) != JournalReadFiles {
		t.Fatalf("Read = %d entries, %v, want %d", len(log.Entries), err, JournalReadFiles)
	}
	for _, entry := range log.Entries {
		if entry.Path == "s0" {
			t.Fatal("the least recently written file was read")
		}
	}
	if mine, err := journal.Read(Filter{Session: "s0"}, 0); err != nil || len(mine.Entries) != 1 {
		t.Fatalf("a filter on the session still reads its file: %+v, %v", mine, err)
	}
}
