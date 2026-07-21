package bridge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// TestResolveAttachmentsFilePassthrough verifies a staged file:// image is passed
// through by path without any network fetch.
func TestResolveAttachmentsFilePassthrough(t *testing.T) {
	dir := t.TempDir()
	img := filepath.Join(dir, "paste-0.png")
	if err := os.WriteFile(img, []byte("PNG"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := contracts.Message{
		ID: "1",
		Attachments: []contracts.Attachment{
			{Filename: "paste-0.png", URL: "file://" + img, ContentType: "image/png"},
		},
	}
	got := ResolveAttachments(context.Background(), nil, m, "sess", nil)
	if len(got) != 1 || got[0] != img {
		t.Fatalf("file:// image must pass through by path, got %v", got)
	}
}

// TestResolveAttachmentsSkipsMissingFile drops a file:// url whose target does not
// exist rather than failing the turn.
func TestResolveAttachmentsSkipsMissingFile(t *testing.T) {
	m := contracts.Message{
		ID: "1",
		Attachments: []contracts.Attachment{
			{Filename: "gone.png", URL: "file:///nonexistent/gone.png", ContentType: "image/png"},
		},
	}
	if got := ResolveAttachments(context.Background(), nil, m, "sess", nil); len(got) != 0 {
		t.Fatalf("missing file must be skipped, got %v", got)
	}
}

// TestResolveAttachmentsSkipsNonImageFile drops a file:// url that is not an image.
func TestResolveAttachmentsSkipsNonImageFile(t *testing.T) {
	dir := t.TempDir()
	txt := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(txt, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := contracts.Message{
		ID:          "1",
		Attachments: []contracts.Attachment{{Filename: "notes.txt", URL: "file://" + txt}},
	}
	if got := ResolveAttachments(context.Background(), nil, m, "sess", nil); len(got) != 0 {
		t.Fatalf("non-image file must be skipped, got %v", got)
	}
}

// TestResolveAttachmentsMixed resolves a file:// image and a CDN image in order,
// passing through the first and downloading the second.
func TestResolveAttachmentsMixed(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("CDNPNG"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	local := filepath.Join(dir, "local.png")
	if err := os.WriteFile(local, []byte("LOCAL"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := contracts.Message{
		ID: "7",
		Attachments: []contracts.Attachment{
			{Filename: "local.png", URL: "file://" + local, ContentType: "image/png"},
			{Filename: "remote.png", URL: srv.URL + "/remote.png", ContentType: "image/png"},
		},
	}
	hosts := map[string]bool(hostsFor(t, srv))
	got := ResolveAttachments(context.Background(), srv.Client(), m, "sess", hosts)
	if len(got) != 2 {
		t.Fatalf("want 2 resolved paths, got %d: %v", len(got), got)
	}
	if got[0] != local {
		t.Errorf("first path must be the passed-through local file, got %s", got[0])
	}
	if b, err := os.ReadFile(got[1]); err != nil || string(b) != "CDNPNG" {
		t.Errorf("second path must be the downloaded CDN image, content=%q err=%v", b, err)
	}
}

// TestResolveAttachmentsOffAllowlistCDNSkipped confirms a CDN image whose host is
// not allow-listed is skipped (the terminal path, which supplies no hosts, thus
// never downloads).
func TestResolveAttachmentsOffAllowlistCDNSkipped(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("PNG"))
	}))
	defer srv.Close()
	m := contracts.Message{
		ID:          "1",
		Attachments: []contracts.Attachment{{Filename: "x.png", URL: srv.URL + "/x.png", ContentType: "image/png"}},
	}
	if got := ResolveAttachments(context.Background(), srv.Client(), m, "sess", nil); len(got) != 0 {
		t.Fatalf("off-allowlist CDN image must be skipped, got %v", got)
	}
}
