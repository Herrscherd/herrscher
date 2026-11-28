package usage

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/testsupport/stagingtest"
)

func TestRefreshThatWritesSweepsAStaleStagingFileAndKeepsAFreshOne(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte(priceList)); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)
	dir := t.TempDir()
	path := filepath.Join(dir, "prices.json")
	stale := filepath.Join(dir, ".prices.json.4242530")
	fresh := filepath.Join(dir, ".prices.json.4242531")
	stagingtest.Plant(t, stale, time.Hour)
	stagingtest.Plant(t, fresh, time.Second)
	source := &PriceSource{Path: path, URL: server.URL, Client: server.Client(), Now: time.Now}

	if err := source.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	stagingtest.Absent(t, stale)
	stagingtest.Present(t, fresh)
	stagingtest.Present(t, path)
}

func TestReadPathNeverTouchesTheStagingFilesOfThePriceFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prices.json")
	if err := os.WriteFile(path, []byte(priceList), 0o600); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(dir, ".prices.json.4242532")
	stagingtest.Plant(t, stale, time.Hour)
	source := &PriceSource{Path: path, Now: time.Now}

	source.due()
	source.loadFile()

	if source.Book().Empty() {
		t.Fatal("the read path did not load the price file")
	}
	stagingtest.Present(t, stale)
}
