package tui

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

var errNotImageType = errors.New("not an image type")

type clipboard interface {
	ImageType(ctx context.Context) (string, bool)
	ReadImage(ctx context.Context, mime string) ([]byte, error)
	WriteText(ctx context.Context, s string) error
}

type wlClipboard struct{}

func newClipboard() clipboard { return wlClipboard{} }

func preferredImageTypes() []string {
	return []string{mimePNG, mimeJPEG, mimeWebP, mimeGIF}
}

func (wlClipboard) ImageType(ctx context.Context) (string, bool) {
	out, err := exec.CommandContext(ctx, "wl-paste", "--list-types").Output()
	if err != nil {
		return "", false
	}
	available := map[string]bool{}
	for _, t := range strings.Fields(string(out)) {
		available[strings.ToLower(strings.TrimSpace(t))] = true
	}
	for _, t := range preferredImageTypes() {
		if available[t] {
			return t, true
		}
	}
	for t := range available {
		if strings.HasPrefix(t, "image/") {
			return t, true
		}
	}
	return "", false
}

func (wlClipboard) ReadImage(ctx context.Context, mime string) ([]byte, error) {
	if !strings.HasPrefix(mime, "image/") {
		return nil, fmt.Errorf("%w: %q", errNotImageType, mime)
	}
	return exec.CommandContext(ctx, "wl-paste", "--type", mime).Output()
}

func (wlClipboard) WriteText(ctx context.Context, s string) error {
	cmd := exec.CommandContext(ctx, "wl-copy")
	cmd.Stdin = strings.NewReader(s)
	if err := cmd.Start(); err != nil {
		return err
	}
	go reap(cmd)
	return nil
}
