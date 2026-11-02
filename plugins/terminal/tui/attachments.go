package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Herrscherd/herrscher/core/bridge"
)

const (
	bytesPerMiB                 = 1 << 20
	bytesPerKiB                 = 1 << 10
	privateDirMode  os.FileMode = 0o700
	privateFileMode os.FileMode = 0o600
)

const (
	mimePNG  = "image/png"
	mimeJPEG = "image/jpeg"
	mimeWebP = "image/webp"
	mimeGIF  = "image/gif"
	extPNG   = ".png"
	extJPG   = ".jpg"
	extWebP  = ".webp"
	extGIF   = ".gif"
)

// Attachment is a file staged to send with the next message (and echoed under the
// user's transcript entry once sent). Path is a local filesystem path; the
// terminal gateway hands it to the host as a file:// URL.
var (
	errEmptyClipboard = errors.New("clipboard image is empty")
	errSizeLimit      = errors.New("exceeds")
	errUsage          = errors.New("usage: /attach <path>")
	errNotRegularFile = errors.New("not a regular file")
)

type Attachment struct {
	Name string
	Path string
	Mime string
	Size int64
}

const maxAttachmentBytes = 10 << 20

func attachmentDir() string {
	return filepath.Join(bridge.StagingRoot(), "terminal")
}

func saveClipboardImage(data []byte, mime string, seq int) (Attachment, error) {
	if len(data) == 0 {
		return Attachment{}, errEmptyClipboard
	}
	if int64(len(data)) > maxAttachmentBytes {
		return Attachment{}, fmt.Errorf("pasted image %w %d MiB", errSizeLimit, maxAttachmentBytes/bytesPerMiB)
	}
	name := fmt.Sprintf("paste-%d%s", seq, mimeExt(mime))
	path, err := stage(name, data)
	if err != nil {
		return Attachment{}, err
	}
	return Attachment{Name: name, Path: path, Mime: mime, Size: int64(len(data))}, nil
}

func stage(name string, data []byte) (string, error) {
	dir := attachmentDir()
	if err := os.MkdirAll(dir, privateDirMode); err != nil {
		return "", err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return "", err
	}
	defer func() { bestEffort(root.Close()) }()
	if err := root.WriteFile(name, data, privateFileMode); err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

func attachLocalFile(path string, seq int) (Attachment, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return Attachment{}, errUsage
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
		return Attachment{}, fmt.Errorf("attach %s: %w", path, errNotRegularFile)
	}
	if info.Size() > maxAttachmentBytes {
		return Attachment{}, fmt.Errorf("attach %s: %w %d MiB", path, errSizeLimit, maxAttachmentBytes/bytesPerMiB)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	data, err := os.ReadFile(filepath.Clean(abs))
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
	case mimePNG:
		return extPNG
	case mimeJPEG:
		return extJPG
	case mimeWebP:
		return extWebP
	case mimeGIF:
		return extGIF
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

func chipRow(atts []Attachment, st uiStyles) string {
	if len(atts) == 0 {
		return ""
	}
	chips := make([]string, len(atts))
	for i, a := range atts {
		chips[i] = st.chip.Render(fmt.Sprintf("📎 %s · %s", a.Name, humanSize(a.Size)))
	}
	return strings.Join(chips, " ")
}

func humanSize(n int64) string {
	switch {
	case n >= bytesPerMiB:
		return fmt.Sprintf("%.1fMB", float64(n)/bytesPerMiB)
	case n >= bytesPerKiB:
		return fmt.Sprintf("%.0fKB", float64(n)/bytesPerKiB)
	default:
		return fmt.Sprintf("%dB", n)
	}
}
