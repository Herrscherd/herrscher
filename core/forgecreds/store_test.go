package forgecreds

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func mustHost(t *testing.T, name string) contracts.ForgeHost {
	t.Helper()
	host, err := contracts.ParseForgeHost(name)
	if err != nil {
		t.Fatalf("ParseForgeHost(%q): %v", name, err)
	}
	return host
}

func TestSavedRecordFileIsReadableByItsOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes carry no owner-only meaning on windows")
	}
	store := NewStore(t.TempDir())
	if err := store.Put(t.Context(), mustHost(t, "github.com"), Record{AccessToken: "ghu_secret"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	info, err := os.Stat(store.Path())
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("file mode = %o, want 600", mode)
	}
}

func TestInsecureReportsAFileOtherUsersCanRead(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes carry no owner-only meaning on windows")
	}
	store := NewStore(t.TempDir())
	if insecure, err := store.Insecure(); err != nil || insecure {
		t.Fatalf("Insecure without a file = %v, %v; want false", insecure, err)
	}
	if err := store.Put(t.Context(), mustHost(t, "github.com"), Record{AccessToken: "ghu_secret"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := os.Chmod(store.Path(), 0o644); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	insecure, err := store.Insecure()
	if err != nil || !insecure {
		t.Fatalf("Insecure after chmod 0644 = %v, %v; want true", insecure, err)
	}
	if err := store.Put(t.Context(), mustHost(t, "github.com"), Record{AccessToken: "ghu_secret"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if insecure, err := store.Insecure(); err != nil || insecure {
		t.Fatalf("Insecure after a save = %v, %v; want false", insecure, err)
	}
}

func TestSavedRecordSurvivesAReopenedStore(t *testing.T) {
	dir := t.TempDir()
	host := mustHost(t, "github.com")
	if err := NewStore(dir).Put(t.Context(), host, Record{AccessToken: "ghu_secret", Login: "octo"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	record, found, err := NewStore(dir).Get(host)
	if err != nil || !found {
		t.Fatalf("Get after reopening = found %v, err %v", found, err)
	}
	if record.AccessToken != "ghu_secret" || record.Login != "octo" {
		t.Fatalf("Get after reopening = %+v, want the saved record", record)
	}
}

func TestDeleteReportsWhetherARecordExisted(t *testing.T) {
	store := NewStore(t.TempDir())
	host := mustHost(t, "github.com")
	if removed, err := store.Delete(t.Context(), host); err != nil || removed {
		t.Fatalf("Delete with no file = %v, %v; want false, nil", removed, err)
	}
	if err := store.Put(t.Context(), host, Record{AccessToken: "ghu_secret"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if removed, err := store.Delete(t.Context(), host); err != nil || !removed {
		t.Fatalf("Delete of a saved record = %v, %v; want true, nil", removed, err)
	}
	if _, found, err := store.Get(host); err != nil || found {
		t.Fatalf("Get after Delete = found %v, err %v; want no record", found, err)
	}
}

func TestDeleteRemovesAnUnreadableFileWhole(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := os.WriteFile(store.Path(), []byte(`{"hosts": {"github.com": {"access_token": "ghu_LEFT_BEHIND"`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	removed, err := store.Delete(t.Context(), mustHost(t, "github.com"))
	if err != nil || !removed {
		t.Fatalf("Delete of a corrupt file = %v, %v; want true, nil", removed, err)
	}
	if _, err := os.Stat(store.Path()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("corrupt file still present after Delete: %v", err)
	}
}

func TestCorruptConnectionsFileIsRefusedWithoutEchoingItsContent(t *testing.T) {
	store := NewStore(t.TempDir())
	const leaked = "ghu_CORRUPT_SECRET"
	if err := os.WriteFile(store.Path(), []byte(`{"hosts": `+leaked), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, _, err := store.Get(mustHost(t, "github.com"))
	if !errors.Is(err, ErrStoreCorrupt) {
		t.Fatalf("Get on a corrupt file = %v, want ErrStoreCorrupt", err)
	}
	if strings.Contains(err.Error(), leaked) {
		t.Fatalf("the error %q carries the file content", err)
	}
	if !strings.Contains(err.Error(), store.Path()) {
		t.Fatalf("the error %q does not name the file", err)
	}
}

func TestNoTempFileIsLeftBesideTheConnectionsFile(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	if err := store.Put(t.Context(), mustHost(t, "github.com"), Record{AccessToken: "ghu_secret"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "."+storeFileName+".") && !strings.HasSuffix(entry.Name(), ".lock") {
			t.Fatalf("temp file %q left after a save", entry.Name())
		}
	}
}

func TestFailedWriteKeepsTheExistingFileAndLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	host := mustHost(t, "github.com")
	if err := store.Put(t.Context(), host, Record{AccessToken: "ghu_original"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	before, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	blockWrites(t, store)
	if putErr := store.Put(t.Context(), host, Record{AccessToken: "ghu_replacement"}); putErr == nil {
		t.Fatalf("Put with a failing rename returned no error")
	}
	after, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatalf("ReadFile after a failed write: %v", err)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("file changed by a failed write:\n%s\nwant:\n%s", after, before)
	}
	record, found, err := NewStore(dir).Get(host)
	if err != nil || !found || record.AccessToken != "ghu_original" {
		t.Fatalf("Get after a failed write = %+v, found %v, err %v; want the original", record, found, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "."+storeFileName+".") && !strings.HasSuffix(entry.Name(), ".lock") {
			t.Fatalf("temp file %q left after a failed write", entry.Name())
		}
	}
}

func TestPredictableTempNameCannotRedirectTheWrite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links need privileges on windows")
	}
	dir := t.TempDir()
	victim := filepath.Join(t.TempDir(), "victim.txt")
	const original = "untouched"
	if err := os.WriteFile(victim, []byte(original), 0o600); err != nil {
		t.Fatalf("WriteFile victim: %v", err)
	}
	guessed := filepath.Join(dir, "."+storeFileName+".tmp")
	if err := os.Symlink(victim, guessed); err != nil {
		t.Fatalf("Symlink: %v", err)
	}
	store := NewStore(dir)
	if err := store.Put(t.Context(), mustHost(t, "github.com"), Record{AccessToken: "ghu_secret"}); err != nil {
		t.Fatalf("Put with a planted link: %v", err)
	}
	got, err := os.ReadFile(victim)
	if err != nil {
		t.Fatalf("ReadFile victim: %v", err)
	}
	if string(got) != original {
		t.Fatalf("the victim file was written through the link: %q", got)
	}
}

func TestRecordExpiryFollowsTheRefreshTokenWhenThereIsOne(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	withRefresh := Record{
		AccessToken:      "ghu",
		RefreshToken:     "ghr",
		AccessExpiresAt:  now.Add(-time.Hour),
		RefreshExpiresAt: now.Add(time.Hour),
	}
	if withRefresh.Expired(now) {
		t.Fatalf("a record whose refresh token is valid reports expired")
	}
	withRefresh.RefreshExpiresAt = now.Add(-time.Minute)
	if !withRefresh.Expired(now) {
		t.Fatalf("a record whose refresh token ran out does not report expired")
	}
	withoutRefresh := Record{AccessToken: "ghu", AccessExpiresAt: now.Add(-time.Minute)}
	if !withoutRefresh.Expired(now) {
		t.Fatalf("a record without refresh token whose access token ran out does not report expired")
	}
}

func TestRenewSoonWarnsInsideTheWindowOnly(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	record := Record{RefreshToken: "ghr", RefreshExpiresAt: now.Add(10 * 24 * time.Hour)}
	if !record.RenewSoon(now) {
		t.Fatalf("a refresh token that runs out in 10 days does not warn")
	}
	record.RefreshExpiresAt = now.Add(30 * 24 * time.Hour)
	if record.RenewSoon(now) {
		t.Fatalf("a refresh token that runs out in 30 days warns")
	}
}
