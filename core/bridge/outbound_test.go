package bridge

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/outbox"
)

const outboundTestMaxBytes int64 = 1024

type recordingBackend struct {
	produce func(dir string) []contracts.OutboundFile
	outbox  string
	files   []contracts.OutboundFile
}

func (b *recordingBackend) Respond(_ context.Context, p contracts.Prompt, _ func(contracts.BackendEvent)) (string, error) {
	b.outbox = p.Outbox
	if b.produce != nil {
		b.files = b.produce(p.Outbox)
	}
	return "done", nil
}

func (b *recordingBackend) Close() error { return nil }

type producingBackend struct{ *recordingBackend }

func (b producingBackend) TakeFiles() []contracts.OutboundFile { return b.files }

type outboundCase struct {
	name        string
	produce     func(t *testing.T, dir string) []contracts.OutboundFile
	wantFiles   int
	wantRefused []string
	wantFrame   bool
	wantDirKept bool
}

func outboundPNG(t *testing.T, dir, name string, size int) contracts.OutboundFile {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, make([]byte, size), 0o600); err != nil {
		t.Fatal(err)
	}
	return contracts.OutboundFile{Path: path, Name: name, ContentType: "image/png"}
}

func outboundFrames(events []contracts.Event) ([]outbox.Frame, error) {
	var frames []outbox.Frame
	for i := range events {
		if events[i].T != outbox.EventFiles {
			continue
		}
		frame, err := outbox.Decode(events[i].Value)
		if err != nil {
			return nil, err
		}
		frames = append(frames, frame)
	}
	return frames, nil
}

func runOutboundTurn(t *testing.T, backend contracts.Backend, root string) *recordSink {
	t.Helper()
	sink := &recordSink{}
	aff := affordances{outbox: outboundSettings{root: root, session: "chan", maxBytes: outboundTestMaxBytes}}
	runOneTurn(context.Background(), sink, backend, nil, contracts.Event{T: "input", Text: "go"}, &turnController{}, nil, aff, nil, 0)
	return sink
}

func assertOutboundFrame(t *testing.T, tc outboundCase, turnDir string, events []contracts.Event) {
	t.Helper()
	frames, err := outboundFrames(events)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(frames) == 1; got != tc.wantFrame {
		t.Fatalf("sent outbound frame = %v, want %v (events %+v)", got, tc.wantFrame, events)
	}
	if tc.wantFrame {
		assertFrameContent(t, tc, frames[0], turnDir)
	}
	if _, err := os.Stat(turnDir); (err == nil) != tc.wantDirKept {
		t.Fatalf("turn directory kept = %v, want %v", err == nil, tc.wantDirKept)
	}
}

func assertFrameContent(t *testing.T, tc outboundCase, frame outbox.Frame, turnDir string) {
	t.Helper()
	if frame.Dir != turnDir {
		t.Fatalf("frame dir %q, want turn directory %q", frame.Dir, turnDir)
	}
	if len(frame.Files) != tc.wantFiles {
		t.Fatalf("frame carries %d files, want %d", len(frame.Files), tc.wantFiles)
	}
	if !slices.Equal(frame.Refused, tc.wantRefused) {
		t.Fatalf("frame refused %v, want %v", frame.Refused, tc.wantRefused)
	}
}

func TestTurnOutboundFiles(t *testing.T) {
	cases := []outboundCase{
		{
			name: "two PNG files are sent to the host with their turn directory",
			produce: func(t *testing.T, dir string) []contracts.OutboundFile {
				t.Helper()
				return []contracts.OutboundFile{outboundPNG(t, dir, "a.png", 8), outboundPNG(t, dir, "b.png", 8)}
			},
			wantFiles:   2,
			wantFrame:   true,
			wantDirKept: true,
		},
		{
			name: "file outside the outbox is refused and named",
			produce: func(t *testing.T, _ string) []contracts.OutboundFile {
				t.Helper()
				return []contracts.OutboundFile{outboundPNG(t, t.TempDir(), "leak.png", 8)}
			},
			wantRefused: []string{"leak.png"},
			wantFrame:   true,
			wantDirKept: true,
		},
		{
			name: "symbolic link leaving the outbox is refused",
			produce: func(t *testing.T, dir string) []contracts.OutboundFile {
				t.Helper()
				target := outboundPNG(t, t.TempDir(), "secret.png", 8)
				link := filepath.Join(dir, "link.png")
				if err := os.Symlink(target.Path, link); err != nil {
					t.Fatal(err)
				}
				return []contracts.OutboundFile{{Path: link, Name: "link.png", ContentType: "image/png"}}
			},
			wantRefused: []string{"link.png"},
			wantFrame:   true,
			wantDirKept: true,
		},
		{
			name: "oversize file is refused and the valid one still sent",
			produce: func(t *testing.T, dir string) []contracts.OutboundFile {
				t.Helper()
				return []contracts.OutboundFile{outboundPNG(t, dir, "big.png", int(outboundTestMaxBytes)+1), outboundPNG(t, dir, "ok.png", 8)}
			},
			wantFiles:   1,
			wantRefused: []string{"big.png"},
			wantFrame:   true,
			wantDirKept: true,
		},
		{
			name: "more than ten files keeps the first ten",
			produce: func(t *testing.T, dir string) []contracts.OutboundFile {
				t.Helper()
				files := make([]contracts.OutboundFile, 0, outbox.MaxFiles+1)
				for i := range outbox.MaxFiles + 1 {
					files = append(files, outboundPNG(t, dir, "f"+string(rune('a'+i))+".png", 8))
				}
				return files
			},
			wantFiles:   outbox.MaxFiles,
			wantRefused: []string{"fk.png"},
			wantFrame:   true,
			wantDirKept: true,
		},
		{
			name: "no file and no refusal sends no frame and removes the turn directory",
			produce: func(*testing.T, string) []contracts.OutboundFile {
				return nil
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			rb := &recordingBackend{produce: func(dir string) []contracts.OutboundFile {
				return tc.produce(t, dir)
			}}
			sink := runOutboundTurn(t, producingBackend{rb}, root)
			if rb.outbox == "" {
				t.Fatal("FileProducer turn got no outbox directory in its prompt")
			}
			assertOutboundFrame(t, tc, rb.outbox, sink.events)
		})
	}
}

func TestBackendWithoutFileProducerGetsNoOutbox(t *testing.T) {
	root := t.TempDir()
	backend := &recordingBackend{}
	sink := runOutboundTurn(t, backend, root)

	if backend.outbox != "" {
		t.Fatalf("prompt outbox = %q, want empty for a backend that renders no files", backend.outbox)
	}
	frames, err := outboundFrames(sink.events)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 0 {
		t.Fatalf("sent %d outbound frames for a backend without FileProducer", len(frames))
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("outbox root got %d entries for a backend without FileProducer", len(entries))
	}
}
