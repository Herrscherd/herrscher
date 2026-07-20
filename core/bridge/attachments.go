package bridge

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/Herrscherd/herrscher-contracts"
)

// imageExts are the filename extensions treated as images when an attachment
// carries no content-type.
var imageExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
	".webp": true, ".bmp": true,
}

// isImage prefers the declared content-type and falls back to the filename
// extension, so an image with an odd or missing extension is still recognized.
func isImage(a contracts.Attachment) bool {
	if a.ContentType != "" {
		return strings.HasPrefix(strings.ToLower(a.ContentType), "image/")
	}
	return imageExts[strings.ToLower(filepath.Ext(a.Filename))]
}

// maxAttachmentBytes bounds a single downloaded image. Anything larger is
// skipped: the bridge must never let an oversized upload stall or OOM a turn.
const maxAttachmentBytes = 10 << 20 // 10 MiB

// maxImagesPerMessage caps how many images one message can pull down, so an
// author can't fan a single message into an unbounded number of fetches/files.
const maxImagesPerMessage = 8

// allowedHosts is the SSRF allowlist for attachment downloads: the caller (the
// gateway that produced the message) supplies the CDN hosts its attachments may
// point at, so the core pins host/scheme without knowing any concrete platform.
// A gateway populates attachments[].url itself, but we still pin it so a future
// change (or a spoofed field) can't turn this into an SSRF primitive.
type allowedHosts map[string]bool

// attachmentDir is where downloaded images land, namespaced per session so
// concurrent bridges don't collide.
func attachmentDir(session string) string {
	name := session
	if name == "" {
		name = "default"
	}
	return filepath.Join(os.TempDir(), "herrscher-attachments", sanitize(name))
}

// downloadImages fetches up to maxImagesPerMessage image attachments on m to
// local files and returns their paths in message order. Non-image, oversized,
// and off-allowlist attachments are skipped (hosts names the CDN hosts a URL may
// point at). Download is best-effort: the successfully fetched paths are always
// returned, alongside the first fetch error encountered (the rest are dropped)
// so a turn is never lost over an image.
func downloadImages(ctx context.Context, client *http.Client, m contracts.Message, dir string, hosts allowedHosts) ([]string, error) {
	imgs := make([]contracts.Attachment, 0, maxImagesPerMessage)
	for _, a := range m.Attachments {
		// Skip oversized uploads before connecting: never stream one up to the cap
		// only to discard it. Size 0 means "unknown" and falls through to the
		// streaming guard in fetchOne.
		if !isImage(a) || (a.Size > 0 && a.Size > maxAttachmentBytes) {
			continue
		}
		imgs = append(imgs, a)
		if len(imgs) == maxImagesPerMessage {
			break
		}
	}
	if len(imgs) == 0 {
		return nil, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("attachment dir: %w", err)
	}
	if client == nil {
		client = http.DefaultClient
	}
	paths := make([]string, 0, len(imgs))
	var firstErr error
	for i, a := range imgs {
		p, err := fetchOne(ctx, client, a, m.ID, i, dir, hosts)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		paths = append(paths, p)
	}
	return paths, firstErr
}

func fetchOne(ctx context.Context, client *http.Client, a contracts.Attachment, msgID string, idx int, dir string, hosts allowedHosts) (string, error) {
	if err := validateCDNURL(a.URL, hosts); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return "", fmt.Errorf("attachment request %s: %w", a.Filename, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", a.Filename, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: status %d", a.Filename, resp.StatusCode)
	}
	// Include the per-message index so two same-named images on one message don't
	// clobber each other (msgID alone collides within a message).
	dest := filepath.Join(dir, fmt.Sprintf("%s-%d-%s", msgID, idx, sanitize(a.Filename)))
	f, err := os.Create(dest)
	if err != nil {
		return "", fmt.Errorf("create %s: %w", dest, err)
	}
	// Bound the copy so a server lying about Size can't exhaust the disk. Read one
	// byte past the cap so an oversized body is detected and skipped rather than
	// silently truncated into a corrupt-but-valid file.
	n, copyErr := io.Copy(f, io.LimitReader(resp.Body, maxAttachmentBytes+1))
	closeErr := f.Close()
	if copyErr != nil {
		os.Remove(dest) // don't leave a truncated image behind
		return "", fmt.Errorf("download %s: %w", a.Filename, copyErr)
	}
	if closeErr != nil {
		os.Remove(dest)
		return "", fmt.Errorf("write %s: %w", dest, closeErr)
	}
	if n > maxAttachmentBytes {
		os.Remove(dest)
		return "", fmt.Errorf("download %s: exceeds %d bytes", a.Filename, maxAttachmentBytes)
	}
	return dest, nil
}

// validateCDNURL pins an attachment URL to https on one of the caller-supplied
// allowlist hosts, rejecting anything else before it is fetched.
func validateCDNURL(raw string, hosts allowedHosts) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("attachment url %q: %w", raw, err)
	}
	if u.Scheme != "https" || !hosts[u.Hostname()] {
		return fmt.Errorf("attachment url %q: not an allowed CDN https url", raw)
	}
	return nil
}

// sanitize keeps a path component to a safe, flat token so a crafted filename or
// session name can't escape the attachment directory.
func sanitize(s string) string {
	s = filepath.Base(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := strings.TrimLeft(b.String(), ".")
	if out == "" {
		return "file"
	}
	return out
}
