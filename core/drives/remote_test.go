package drives

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAKeyRemoteKeepsItsSecretInHerrschersConfigAlone(t *testing.T) {
	catalog := Open(filepath.Join(t.TempDir(), "drives"))
	name, err := ParseName("r2")
	if err != nil {
		t.Fatal(err)
	}
	remote := KeyRemote{Kind: KindR2, AccessKeyID: "AKID", Secret: "s3cr/t+key=", Endpoint: R2Endpoint("acct")}
	if addErr := catalog.AddKeyRemote(t.Context(), name, remote); addErr != nil {
		t.Fatalf("AddKeyRemote: %v", addErr)
	}
	raw, err := os.ReadFile(catalog.RcloneConfig())
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"[r2]", "type = s3", "provider = Cloudflare", "secret_access_key = s3cr/t+key=", "endpoint = https://acct.r2.cloudflarestorage.com"} {
		if !strings.Contains(string(raw), line+"\n") {
			t.Errorf("config lacks %q:\n%s", line, raw)
		}
	}
	info, err := os.Stat(catalog.RcloneConfig())
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %v, %v, want 0600", info.Mode().Perm(), err)
	}
	if err := catalog.AddKeyRemote(t.Context(), name, remote); !errors.Is(err, ErrRemoteExists) {
		t.Fatalf("a second remote of the same name: err = %v, want ErrRemoteExists", err)
	}
}

func TestAKeyRemoteRefusesAValueThatCouldBreakTheSection(t *testing.T) {
	catalog := Open(filepath.Join(t.TempDir(), "drives"))
	name, err := ParseName("s3")
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"", "has space", "x\n[evil]", "x;y", "x#y", `x"y`} {
		err := catalog.AddKeyRemote(t.Context(), name, KeyRemote{Kind: KindS3, AccessKeyID: "AKID", Secret: secret})
		if _, bad := errors.AsType[*BadSettingError](err); !bad {
			t.Errorf("secret %q: err = %v, want a BadSettingError", secret, err)
		}
	}
	if err := catalog.AddKeyRemote(t.Context(), name, KeyRemote{Kind: KindGoogleDrive}); !errors.Is(err, ErrNotKeyKind) {
		t.Fatalf("an OAuth kind: err = %v, want ErrNotKeyKind", err)
	}
}

func TestTheJournalRotatesAndReadsAcrossTheRotation(t *testing.T) {
	journal := Open(t.TempDir()).Journal()
	padding := strings.Repeat("x", journalRotateBytes/2)
	for index := range 3 {
		entry := Entry{Drive: "gdrive", Session: "s1", Op: OpRead, Outcome: OutcomeDone, Reason: padding}
		if index == 2 {
			entry.Session = "s2"
		}
		if err := journal.Append(t.Context(), entry); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(journal.path + rotatedSuffix); err != nil {
		t.Fatalf("no rotated journal: %v", err)
	}
	all, err := journal.Read(Filter{}, 0)
	if err != nil || len(all) != 3 {
		t.Fatalf("Read = %d entries, %v, want 3", len(all), err)
	}
	mine, err := journal.Read(Filter{Session: "s2"}, 0)
	if err != nil || len(mine) != 1 {
		t.Fatalf("Read of s2 = %+v, %v", mine, err)
	}
	last, err := journal.Read(Filter{}, 1)
	if err != nil || len(last) != 1 || last[0].Session != "s2" {
		t.Fatalf("Read limit 1 = %+v, %v, want the newest", last, err)
	}
}
