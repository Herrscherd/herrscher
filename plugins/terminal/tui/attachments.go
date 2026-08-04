package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Herrscherd/herrscher/core/bridge"
)

// Attachment is a file staged to send with the next message (and echoed under the
// user's transcript entry once sent). Path is a local filesystem path; the
// terminal gateway hands it to the host as a file:// URL.
type Attachment struct {
	Name string
	Path string
	Mime string
	Size int64
}

// maxAttachmentBytes bounds a staged file, mirroring the bridge download cap so a
// pasted image the host would reject never gets queued in the first place.
const maxAttachmentBytes = 10 << 20 // 10 MiB

// attachmentDir is where staged files are written: the bridge's staging root,
// namespaced for this gateway. The root comes from the bridge rather than a copy
// of the path here, because the bridge refuses any file:// attachment from
// outside it.
func attachmentDir() string {
	return filepath.Join(bridge.StagingRoot(), "terminal")
}

// saveClipboardImage writes clipboard image bytes to a fresh temp file and returns
// the Attachment describing it. seq disambiguates names within a session.
func saveClipboardImage(data []byte, mime string, seq int) (Attachment, error) {
	if len(data) == 0 {
		return Attachment{}, fmt.Errorf("clipboard image is empty")
	}
	if int64(len(data)) > maxAttachmentBytes {
		return Attachment{}, fmt.Errorf("pasted image exceeds %d MiB", maxAttachmentBytes>>20)
	}
	name := fmt.Sprintf("paste-%d%s", seq, mimeExt(mime))
	path, err := stage(name, data)
	if err != nil {
		return Attachment{}, err
	}
	return Attachment{Name: name, Path: path, Mime: mime, Size: int64(len(data))}, nil
}

// stage writes bytes into the staging dir under the given name and returns the
// path. The host only accepts a file:// attachment from under this tree, so
// staging is what makes a file nameable at all — and the modes are tight because
// the tree lives in a world-writable temp dir.
func stage(name string, data []byte) (string, error) {
	dir := attachmentDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// attachLocalFile stages an existing local file for /attach, validating it is a
// regular file within the size cap and resolving ~ and relative paths. seq
// disambiguates two attachments sharing a basename.
func attachLocalFile(path string, seq int) (Attachment, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return Attachment{}, fmt.Errorf("usage: /attach <path>")
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, path[2:])
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		return Attachment{}, fmt.Errorf("attach %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return Attachment{}, fmt.Errorf("attach %s: not a regular file", path)
	}
	if info.Size() > maxAttachmentBytes {
		return Attachment{}, fmt.Errorf("attach %s: exceeds %d MiB", path, maxAttachmentBytes>>20)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	// Copy rather than point at the original. The host refuses a file:// path from
	// outside the staging tree — "the gateway staged this" is something it checks,
	// not something it takes our word for — and a copy also means the turn reads
	// what was attached rather than whatever the file became while it queued.
	data, err := os.ReadFile(abs)
	if err != nil {
		return Attachment{}, fmt.Errorf("attach %s: %w", path, err)
	}
	name := filepath.Base(abs)
	staged, err := stage(fmt.Sprintf("%d-%s", seq, name), data)
	if err != nil {
		return Attachment{}, fmt.Errorf("attach %s: %w", path, err)
	}
	return Attachment{Name: name, Path: staged, Mime: mimeByExt(abs), Size: info.Size()}, nil
}

func mimeExt(mime string) string {
	switch mime {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	}
	return ".bin"
}

// mimeByExt names what /attach staged. An extension it doesn't know yields "",
// and the host falls back to the extension itself — so this only has to cover
// what is worth naming, not everything that can be attached.
func mimeByExt(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	case ".pdf":
		return "application/pdf"
	case ".txt", ".log":
		return "text/plain"
	case ".md", ".markdown":
		return "text/markdown"
	case ".csv":
		return "text/csv"
	case ".json":
		return "application/json"
	}
	return ""
}

// chipRow renders staged/echoed attachments as pill tokens.
func chipRow(atts []Attachment) string {
	if len(atts) == 0 {
		return ""
	}
	chips := make([]string, len(atts))
	for i, a := range atts {
		chips[i] = chipStyle.Render(fmt.Sprintf("📎 %s · %s", a.Name, humanSize(a.Size)))
	}
	return strings.Join(chips, " ")
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0fKB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%dB", n)
	}
}
