package tui

import (
	"context"
	"errors"
	"testing"
)

func TestReadImageRefusesNonImageTypes(t *testing.T) {
	for _, mime := range []string{"text/plain", "", "--help"} {
		_, err := wlClipboard{}.ReadImage(context.Background(), mime)
		if !errors.Is(err, errNotImageType) {
			t.Errorf("ReadImage(%q) error = %v, want errNotImageType", mime, err)
		}
	}
}
