package accounts

import (
	"path/filepath"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/testsupport/stagingtest"
)

func TestSetDefaultSweepsAStaleStagingFileOfTheDefaults(t *testing.T) {
	store := newStore(t)
	name := mustName(t, "perso")
	if _, err := store.Create(contracts.VendorClaude, name); err != nil {
		t.Fatalf("Create: %v", err)
	}
	stale := filepath.Join(store.Root, "."+defaultsFileName+".4242570")
	fresh := filepath.Join(store.Root, "."+defaultsFileName+".4242571")
	stagingtest.Plant(t, stale, time.Hour)
	stagingtest.Plant(t, fresh, time.Second)

	if err := store.SetDefault(contracts.VendorClaude, name); err != nil {
		t.Fatalf("SetDefault: %v", err)
	}

	stagingtest.Absent(t, stale)
	stagingtest.Present(t, fresh)
}
