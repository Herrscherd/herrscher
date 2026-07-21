package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// attachmentDir is where pasted images are written, mirroring the bridge's
// per-gateway namespacing under the OS temp dir.
func attachmentDir() string {
	return filepath.Join(os.TempDir(), "herrscher-attachments", "terminal")
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
	dir := attachmentDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Attachment{}, err
	}
	name := fmt.Sprintf("paste-%d%s", seq, mimeExt(mime))
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return Attachment{}, err
	}
	return Attachment{Name: name, Path: path, Mime: mime, Size: int64(len(data))}, nil
}

// attachLocalFile stages an existing local file for /attach, validating it is a
// regular file within the size cap and resolving ~ and relative paths.
func attachLocalFile(path string) (Attachment, error) {
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
	return Attachment{Name: filepath.Base(abs), Path: abs, Mime: mimeByExt(abs), Size: info.Size()}, nil
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
