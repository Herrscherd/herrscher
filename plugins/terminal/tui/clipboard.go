package tui

import (
	"os/exec"
	"strings"
)

type clipboard interface {
	ImageType() (string, bool)
	ReadImage(mime string) ([]byte, error)
	WriteText(s string) error
}

type wlClipboard struct{}

func newClipboard() clipboard { return wlClipboard{} }

func preferredImageTypes() []string {
	return []string{mimePNG, mimeJPEG, mimeWebP, mimeGIF}
}

func (wlClipboard) ImageType() (string, bool) {
	out, err := exec.Command("wl-paste", "--list-types").Output()
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

func (wlClipboard) ReadImage(mime string) ([]byte, error) {
	return exec.Command("wl-paste", "--type", mime).Output()
}

func (wlClipboard) WriteText(s string) error {
	cmd := exec.Command("wl-copy")
	cmd.Stdin = strings.NewReader(s)
	if err := cmd.Start(); err != nil {
		return err
	}
	go reap(cmd)
	return nil
}
