package forgecreds

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/testsupport/stagingtest"
)

func TestUpdateSweepsAStaleStagingFileOfTheStore(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	stale := filepath.Join(dir, "."+storeFileName+".4242550")
	fresh := filepath.Join(dir, "."+storeFileName+".4242551")
	stagingtest.Plant(t, stale, time.Hour)
	stagingtest.Plant(t, fresh, time.Second)

	if err := store.Update(context.Background(), func(*Tx) error { return nil }); err != nil {
		t.Fatalf("Update: %v", err)
	}

	stagingtest.Absent(t, stale)
	stagingtest.Present(t, fresh)
}
