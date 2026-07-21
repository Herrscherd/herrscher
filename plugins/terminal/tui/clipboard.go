package tui

import (
	"os/exec"
	"strings"
)

// clipboard reads image data from the system clipboard. It is an interface so the
// paste path can be driven by a fake in tests, and so a host without a clipboard
// tool degrades to "no image" rather than failing.
type clipboard interface {
	// ImageType returns the preferred image MIME type currently on the clipboard,
	// or ("", false) when the clipboard holds no image.
	ImageType() (string, bool)
	// ReadImage returns the raw bytes of the clipboard image in the given MIME type.
	ReadImage(mime string) ([]byte, error)
}

// wlClipboard reads images via wl-paste (Wayland). Missing binary or non-image
// clipboard content both surface as "no image", never an error the UI must handle.
type wlClipboard struct{}

// newClipboard returns the platform clipboard reader. Wayland/wl-paste is the only
// backend today (the run target is a Wayland/kitty terminal); others degrade to
// no-image via the exec failing.
func newClipboard() clipboard { return wlClipboard{} }

// preferredImageTypes is the priority order for pulling an image off the
// clipboard: lossless PNG first, then common alternates.
var preferredImageTypes = []string{"image/png", "image/jpeg", "image/webp", "image/gif"}

func (wlClipboard) ImageType() (string, bool) {
	out, err := exec.Command("wl-paste", "--list-types").Output()
	if err != nil {
		return "", false
	}
	available := map[string]bool{}
	for _, t := range strings.Fields(string(out)) {
		available[strings.ToLower(strings.TrimSpace(t))] = true
	}
	for _, t := range preferredImageTypes {
		if available[t] {
			return t, true
		}
	}
	// Any other image/* type the app can still tag as an attachment.
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
