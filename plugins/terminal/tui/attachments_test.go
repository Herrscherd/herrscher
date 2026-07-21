package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// fakeClipboard drives the Ctrl+V paste path deterministically: it returns a fixed
// image type and bytes, so tests never touch the real system clipboard.
type fakeClipboard struct {
	mime string
	data []byte
	err  error
}

func (f fakeClipboard) ImageType() (string, bool) {
	if f.mime == "" {
		return "", false
	}
	return f.mime, true
}

func (f fakeClipboard) ReadImage(string) ([]byte, error) { return f.data, f.err }

// TestCtrlVStagesClipboardImage verifies a clipboard image is staged as a pending
// attachment, shown as a chip, and reserves a chrome row.
func TestCtrlVStagesClipboardImage(t *testing.T) {
	m := readyModel(&fakeBackend{})
	m.clip = fakeClipboard{mime: "image/png", data: []byte("\x89PNGfake")}
	base := m.vp.Height

	m.Update(tea.KeyMsg{Type: tea.KeyCtrlV})

	if len(m.pending) != 1 {
		t.Fatalf("Ctrl+V must stage one attachment, got %d", len(m.pending))
	}
	att := m.pending[0]
	if att.Mime != "image/png" || !strings.HasSuffix(att.Path, ".png") {
		t.Fatalf("staged attachment wrong: %+v", att)
	}
	if _, err := os.Stat(att.Path); err != nil {
		t.Fatalf("pasted image must be written to disk: %v", err)
	}
	t.Cleanup(func() { os.Remove(att.Path) })
	if m.vp.Height != base-1 {
		t.Fatalf("chip row must steal one viewport line: base=%d now=%d", base, m.vp.Height)
	}
	if !strings.Contains(m.View(), att.Name) {
		t.Fatalf("staged chip must appear in the view")
	}
}

// TestCtrlVNoImageFallsThrough confirms Ctrl+V with no clipboard image stages
// nothing (so the composer's text paste can handle it instead).
func TestCtrlVNoImageFallsThrough(t *testing.T) {
	m := readyModel(&fakeBackend{})
	m.clip = fakeClipboard{} // empty: no image
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	if len(m.pending) != 0 {
		t.Fatalf("Ctrl+V without an image must stage nothing, got %d", len(m.pending))
	}
}

// TestSubmitCarriesAttachments checks the full wiring: a staged image plus text is
// submitted to the backend with the attachment, echoed under the you turn, and the
// pending set is cleared.
func TestSubmitCarriesAttachments(t *testing.T) {
	f := &fakeBackend{}
	m := readyModel(f)
	m.ensureTab("a")
	m.active = "a"
	m.clip = fakeClipboard{mime: "image/png", data: []byte("bytes")}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	path := m.pending[0].Path
	t.Cleanup(func() { os.Remove(path) })

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("look at this")})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if len(f.submitted) != 1 {
		t.Fatalf("expected one submit, got %d", len(f.submitted))
	}
	got := f.submitted[0]
	if got.text != "look at this" || len(got.atts) != 1 || got.atts[0].Path != path {
		t.Fatalf("submit did not carry text+attachment: %+v", got)
	}
	if len(m.pending) != 0 {
		t.Fatalf("pending must clear after submit, got %d", len(m.pending))
	}
	tb := m.tabs["a"]
	last := tb.entries[len(tb.entries)-1]
	if last.role != roleYou || len(last.attachments) != 1 || last.attachments[0].Path != path {
		t.Fatalf("you turn must echo the attachment: %+v", last)
	}
	if !strings.Contains(renderEntry(last, 80), filepath.Base(path)) {
		t.Fatalf("rendered you turn must show the attachment chip")
	}
}

// TestAttachCommandStagesFile verifies /attach <path> stages an existing local file.
func TestAttachCommandStagesFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "diagram.png")
	if err := os.WriteFile(p, []byte("img"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := readyModel(&fakeBackend{})
	m.ensureTab("a")
	m.active = "a"
	for _, r := range "/attach " + p {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.pending) != 1 || m.pending[0].Path != p {
		t.Fatalf("/attach must stage the file, pending=%+v", m.pending)
	}
}
