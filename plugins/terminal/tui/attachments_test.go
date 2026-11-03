package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

type fakeClipboard struct {
	mime string
	data []byte
	err  error
}

func (f fakeClipboard) ImageType(context.Context) (string, bool) {
	if f.mime == "" {
		return "", false
	}
	return f.mime, true
}

func (f fakeClipboard) ReadImage(context.Context, string) ([]byte, error) { return f.data, f.err }

func (f fakeClipboard) WriteText(context.Context, string) error { return nil }

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
	t.Cleanup(func() { removeIfPresent(t, att.Path) })
	if m.vp.Height != base-1 {
		t.Fatalf("chip row must steal one viewport line: base=%d now=%d", base, m.vp.Height)
	}
	if !strings.Contains(m.View(), att.Name) {
		t.Fatalf("staged chip must appear in the view")
	}
}

func TestCtrlVNoImageFallsThrough(t *testing.T) {
	m := readyModel(&fakeBackend{})
	m.clip = fakeClipboard{}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	if len(m.pending) != 0 {
		t.Fatalf("Ctrl+V without an image must stage nothing, got %d", len(m.pending))
	}
}

func TestSubmitCarriesAttachments(t *testing.T) {
	f := &fakeBackend{}
	m := readyModel(f)
	m.ensureTab("a")
	m.active = "a"
	m.clip = fakeClipboard{mime: "image/png", data: []byte("bytes")}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	path := m.pending[0].Path
	t.Cleanup(func() { removeIfPresent(t, path) })

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
	if !strings.Contains(renderEntry(last, 80, view{}), filepath.Base(path)) {
		t.Fatalf("rendered you turn must show the attachment chip")
	}
}

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
	if len(m.pending) != 1 {
		t.Fatalf("/attach must stage the file, pending=%+v", m.pending)
	}
	got := m.pending[0]
	t.Cleanup(func() { removeIfPresent(t, got.Path) })
	if filepath.Dir(got.Path) != attachmentDir() {
		t.Errorf("staged path %s is not under %s", got.Path, attachmentDir())
	}
	if got.Name != "diagram.png" {
		t.Errorf("chip name = %q, want the original basename", got.Name)
	}
	if b, err := os.ReadFile(got.Path); err != nil || string(b) != "img" {
		t.Errorf("staged copy content = %q err=%v", b, err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Errorf("original file must survive /attach: %v", err)
	}
}

func removeIfPresent(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		t.Error(err)
	}
}

func TestStageRefusesANameThatEscapesTheAttachmentDirectory(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if _, err := stage("../escape.txt", []byte("x")); err == nil {
		t.Fatal("stage accepted a name that leaves the attachment directory")
	}
}
