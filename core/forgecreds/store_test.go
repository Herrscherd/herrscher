package forgecreds

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	store := NewStore(t.TempDir())
	if err := store.Put(mustHost(t, "github.com"), Record{AccessToken: "ghu_secret"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	info, err := os.Stat(store.path())
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("connections file mode = %o, want 600", mode)
	}
}

func TestSavedRecordSurvivesAReopenedStore(t *testing.T) {
	dir := t.TempDir()
	host := mustHost(t, "github.com")
	if err := NewStore(dir).Put(host, Record{AccessToken: "ghu_secret", Login: "octo"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	record, found, err := NewStore(dir).Get(host)
	if err != nil || !found {
		t.Fatalf("Get = found %v, err %v; want the saved record", found, err)
	}
	if record.AccessToken != "ghu_secret" || record.Login != "octo" {
		t.Fatalf("Get returned %+v, want the saved token and login", record)
	}
}

func TestRejectMarksTheSavedTokenWithoutErasingIt(t *testing.T) {
	store := NewStore(t.TempDir())
	host := mustHost(t, "github.com")
	if err := store.Put(host, Record{AccessToken: "ghu_secret"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := store.Reject(host); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	record, found, err := store.Get(host)
	if err != nil || !found {
		t.Fatalf("Get after Reject = found %v, err %v; want the record kept", found, err)
	}
	if !record.Rejected || record.AccessToken != "ghu_secret" {
		t.Fatalf("Get after Reject = %+v, want Rejected set and the token kept", record)
	}
}

func TestDeleteReportsWhetherARecordExisted(t *testing.T) {
	store := NewStore(t.TempDir())
	host := mustHost(t, "github.com")
	if removed, err := store.Delete(host); err != nil || removed {
		t.Fatalf("Delete on an empty store = %v, %v; want false, nil", removed, err)
	}
	if err := store.Put(host, Record{AccessToken: "ghu_secret"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if removed, err := store.Delete(host); err != nil || !removed {
		t.Fatalf("Delete of a saved record = %v, %v; want true, nil", removed, err)
	}
	if _, found, err := store.Get(host); err != nil || found {
		t.Fatalf("the record is still there after Delete")
	}
}

func TestCorruptConnectionsFileIsRefusedWithoutEchoingItsContent(t *testing.T) {
	dir := t.TempDir()
	const leaked = "ghu_LEAKED_TOKEN"
	if err := os.WriteFile(filepath.Join(dir, storeFileName), []byte(`{"hosts": `+leaked), storeFileMode); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, _, err := NewStore(dir).Get(mustHost(t, "github.com"))
	if !errors.Is(err, ErrStoreCorrupt) {
		t.Fatalf("Get on a corrupt file = %v, want ErrStoreCorrupt", err)
	}
	if strings.Contains(err.Error(), leaked) {
		t.Fatalf("the error %q echoes the stored content", err)
	}
}

func TestNoTempFileIsLeftBesideTheConnectionsFile(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	if err := store.Put(mustHost(t, "github.com"), Record{AccessToken: "ghu_secret"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != storeFileName {
		t.Fatalf("state directory holds %d entries after Put, want only %s", len(entries), storeFileName)
	}
}
