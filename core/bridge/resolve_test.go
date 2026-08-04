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

// stagedFile writes a file inside the staging root — the only tree the bridge
// accepts a file:// attachment from, so a gateway has to copy a file there before
// it can name one. It returns the path with symlinks resolved, which is what
// ResolveAttachments hands back.
func stagedFile(t *testing.T, name, content string) string {
	t.Helper()
	if err := os.MkdirAll(StagingRoot(), 0o700); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(StagingRoot(), "test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return real
}

// TestResolveAttachmentsFilePassthrough verifies a staged file:// image is passed
// through by path without any network fetch.
func TestResolveAttachmentsFilePassthrough(t *testing.T) {
	img := stagedFile(t, "paste-0.png", "PNG")
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

// TestResolveAttachmentsSkipsUnsupportedFile drops a staged file:// url whose
// type the model can do nothing with.
func TestResolveAttachmentsSkipsUnsupportedFile(t *testing.T) {
	zip := stagedFile(t, "bundle.zip", "PK")
	m := contracts.Message{
		ID:          "1",
		Attachments: []contracts.Attachment{{Filename: "bundle.zip", URL: "file://" + zip}},
	}
	if got := ResolveAttachments(context.Background(), nil, m, "sess", nil); len(got) != 0 {
		t.Fatalf("unsupported file must be skipped, got %v", got)
	}
}

// TestResolveAttachmentsPassesThroughStagedDocument confirms a document staged by
// a gateway reaches the backend the same way an image does — the path is what the
// backend gets, and it reads the file itself.
func TestResolveAttachmentsPassesThroughStagedDocument(t *testing.T) {
	doc := stagedFile(t, "spec.pdf", "%PDF-1.7")
	m := contracts.Message{
		ID:          "1",
		Attachments: []contracts.Attachment{{Filename: "spec.pdf", URL: "file://" + doc, ContentType: "application/pdf"}},
	}
	got := ResolveAttachments(context.Background(), nil, m, "sess", nil)
	if len(got) != 1 || got[0] != doc {
		t.Fatalf("staged pdf must pass through by path, got %v", got)
	}
}

// TestResolveAttachmentsRejectsUnstagedFile confirms a file:// url naming a real
// image outside the staging tree is refused: a gateway has to copy a file in
// before it can name it, so a compromised one cannot read arbitrary local files
// into the model context.
func TestResolveAttachmentsRejectsUnstagedFile(t *testing.T) {
	img := filepath.Join(t.TempDir(), "secret.png")
	if err := os.WriteFile(img, []byte("PNG"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := contracts.Message{
		ID:          "1",
		Attachments: []contracts.Attachment{{Filename: "secret.png", URL: "file://" + img, ContentType: "image/png"}},
	}
	if got := ResolveAttachments(context.Background(), nil, m, "sess", nil); len(got) != 0 {
		t.Fatalf("unstaged file must be refused, got %v", got)
	}
}

// TestResolveAttachmentsRejectsSymlinkOutOfStaging confirms containment is checked
// after symlinks resolve, so a link planted inside the staging tree cannot be used
// to name a file outside it.
func TestResolveAttachmentsRejectsSymlinkOutOfStaging(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "secret.png")
	if err := os.WriteFile(outside, []byte("PNG"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Dir(stagedFile(t, "anchor", "x")) + "/link.png"
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	m := contracts.Message{
		ID:          "1",
		Attachments: []contracts.Attachment{{Filename: "link.png", URL: "file://" + link, ContentType: "image/png"}},
	}
	if got := ResolveAttachments(context.Background(), nil, m, "sess", nil); len(got) != 0 {
		t.Fatalf("symlink out of the staging tree must be refused, got %v", got)
	}
}

// TestResolveAttachmentsRejectsHostedFileURL confirms a file url with a host
// (file://evil/etc/passwd, whose Path is /etc/passwd) is rejected rather than
// read — only a genuine host-less local file url passes.
func TestResolveAttachmentsRejectsHostedFileURL(t *testing.T) {
	m := contracts.Message{
		ID:          "1",
		Attachments: []contracts.Attachment{{Filename: "passwd", URL: "file://evil/etc/passwd", ContentType: "image/png"}},
	}
	if got := ResolveAttachments(context.Background(), nil, m, "sess", nil); len(got) != 0 {
		t.Fatalf("hosted file url must be rejected, got %v", got)
	}
}

// TestResolveAttachmentsMixed resolves a file:// image and a CDN image in order,
// passing through the first and downloading the second.
func TestResolveAttachmentsMixed(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("CDNPNG"))
	}))
	defer srv.Close()

	local := stagedFile(t, "local.png", "LOCAL")
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
