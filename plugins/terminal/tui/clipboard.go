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
	// WriteText puts s on the clipboard. It is the copy half of the seam: a code
	// block leaves the TUI the same way an image enters it.
	WriteText(s string) error
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

// WriteText copies through wl-copy. It runs detached: wl-copy holds the
// selection until something else claims it, so waiting for it to exit would hang
// the TUI for as long as the copy is useful.
func (wlClipboard) WriteText(s string) error {
	cmd := exec.Command("wl-copy")
	cmd.Stdin = strings.NewReader(s)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() // reaped off the render path; an unwaited child stays a zombie
	return nil
}
