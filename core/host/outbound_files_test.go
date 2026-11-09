package host

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/outbox"
)

const outboundTestMaxBytes int64 = 1 << 20

type textRecordingGateway struct {
	fakeGateway

	sent []string
}

func (g *textRecordingGateway) Reply(_ context.Context, _ contracts.Conversation, _ contracts.MessageID, text string) (contracts.MessageID, error) {
	g.sent = append(g.sent, text)
	return "", nil
}

func (g *textRecordingGateway) Post(_ context.Context, _ contracts.Conversation, text string) (contracts.MessageID, error) {
	g.sent = append(g.sent, text)
	return "", nil
}

type routedFileGateway struct {
	textRecordingGateway

	routed    []contracts.Event
	calls     []string
	sentFiles []string
	sentText  []string
}

func (g *routedFileGateway) EmitTo(_ contracts.Conversation, event contracts.Event) {
	g.routed = append(g.routed, event)
	g.calls = append(g.calls, "emit")
}

func (g *routedFileGateway) SendFiles(_ context.Context, _ contracts.Conversation, _ contracts.MessageID, text string, files []contracts.OutboundFile) (contracts.MessageID, error) {
	g.calls = append(g.calls, "send")
	g.sentText = append(g.sentText, text)
	for _, file := range files {
		g.sentFiles = append(g.sentFiles, file.Name)
	}
	return "", nil
}

func turnFrame(t *testing.T, root string) outbox.Frame {
	t.Helper()
	dir, err := outbox.NewTurnDir(root, "chan")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "a.png")
	if err := os.WriteFile(path, []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}
	return outbox.Frame{
		Dir:   dir,
		Files: []contracts.OutboundFile{{Path: path, Name: "a.png", ContentType: "image/png"}},
	}
}

func newOutboundDriver(gws []contracts.GatewaySet, root string) *sessionDriver {
	d := newSessionDriver("s1", gws, nil, nil)
	d.outboxRoot = root
	d.outboundMaxBytes = outboundTestMaxBytes
	return d
}

func assertTurnDirRemoved(t *testing.T, dir string) {
	t.Helper()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("turn directory %q survived delivery: %v", dir, err)
	}
}

func TestReplyEmitsNormallyThenSendsFilesThroughFileSender(t *testing.T) {
	root := t.TempDir()
	frame := turnFrame(t, root)
	frame.Refused = []string{"big.bin"}
	gw := &routedFileGateway{}
	d := newOutboundDriver([]contracts.GatewaySet{{Gateway: gw}}, root)

	d.deliverReply(context.Background(), contracts.Event{T: "reply", Text: "hello", Done: true}, frame)

	if !slices.Equal(gw.calls, []string{"emit", "send"}) {
		t.Fatalf("calls = %q, want the reply emitted before the files are sent", gw.calls)
	}
	if len(gw.routed) != 1 || !gw.routed[0].Done {
		t.Fatalf("EmitTo got %+v, want the terminal reply once", gw.routed)
	}
	reply := gw.routed[0].Text
	if !strings.HasPrefix(reply, "hello") || !strings.Contains(reply, "big.bin") {
		t.Fatalf("reply text %q, want the reply followed by the refused notice", reply)
	}
	if strings.Contains(reply, "a.png") {
		t.Fatalf("reply text %q names a file that SendFiles carries", reply)
	}
	if !slices.Equal(gw.sentText, []string{""}) {
		t.Fatalf("SendFiles texts = %q, want one empty text", gw.sentText)
	}
	if !slices.Equal(gw.sentFiles, []string{"a.png"}) {
		t.Fatalf("SendFiles files = %q, want [a.png]", gw.sentFiles)
	}
	if len(gw.sent) != 0 {
		t.Fatalf("gateway also got plain messages %q, the reply would be duplicated", gw.sent)
	}
}

func TestReplyNamesUnsentFilesWithoutFileSenderAndNeverPaths(t *testing.T) {
	root := t.TempDir()
	frame := turnFrame(t, root)
	frame.Refused = []string{"big.bin"}
	gw := &textRecordingGateway{}
	d := newOutboundDriver([]contracts.GatewaySet{{Gateway: gw}}, root)

	d.deliverReply(context.Background(), contracts.Event{T: "reply", Text: "hello", Done: true}, frame)

	if len(gw.sent) != 1 {
		t.Fatalf("gateway got %d messages, want one reply message: %q", len(gw.sent), gw.sent)
	}
	text := gw.sent[0]
	for _, want := range []string{"hello", "a.png", "big.bin"} {
		if !strings.Contains(text, want) {
			t.Fatalf("fallback text %q lacks %q", text, want)
		}
	}
	if strings.Count(text, "hello") != 1 {
		t.Fatalf("reply text duplicated in %q", text)
	}
	if strings.Contains(text, frame.Dir) {
		t.Fatalf("fallback text %q leaks the host path of the outbox", text)
	}
}

func TestReplyRevetsFilesBeforeSending(t *testing.T) {
	root := t.TempDir()
	frame := turnFrame(t, root)
	outside := filepath.Join(t.TempDir(), "secret.png")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(frame.Files[0].Path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, frame.Files[0].Path); err != nil {
		t.Fatal(err)
	}
	gw := &routedFileGateway{}
	d := newOutboundDriver([]contracts.GatewaySet{{Gateway: gw}}, root)

	d.deliverReply(context.Background(), contracts.Event{T: "reply", Text: "hello", Done: true}, frame)

	if len(gw.sentFiles) != 0 {
		t.Fatalf("SendFiles got %q, want the file swapped for a link refused", gw.sentFiles)
	}
	if !strings.Contains(gw.routed[0].Text, "a.png") {
		t.Fatalf("reply text %q lacks the notice of the refused file", gw.routed[0].Text)
	}
}

func TestRemoveTurnFilesDeletesTheTurnDirectory(t *testing.T) {
	root := t.TempDir()
	frame := turnFrame(t, root)
	d := newOutboundDriver(nil, root)

	d.removeTurnFiles(frame)

	assertTurnDirRemoved(t, frame.Dir)
}
