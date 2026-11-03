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
	available := make(map[string]bool)
	for _, t := range strings.Fields(string(out)) {
		available[strings.ToLower(strings.TrimSpace(t))] = true
	}
	for _, target := range preferredImageTypes() {
		if available[target] {
			return target, true
		}
	}
	for target := range available {
		if strings.HasPrefix(target, "image/") {
			return target, true
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

func (wlClipboard) WriteText(ctx context.Context, text string) error {
	cmd := exec.CommandContext(ctx, "wl-copy")
	cmd.Stdin = strings.NewReader(text)
	if err := cmd.Start(); err != nil {
		return err
	}
	go reap(cmd)
	return nil
}
