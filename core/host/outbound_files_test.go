package host

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/outbox"
)

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

type fileRecordingGateway struct {
	textRecordingGateway

	sentFiles []string
	sentText  []string
}

func (g *fileRecordingGateway) SendFiles(_ context.Context, _ contracts.Conversation, _ contracts.MessageID, text string, files []contracts.OutboundFile) (contracts.MessageID, error) {
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

func assertTurnDirRemoved(t *testing.T, dir string) {
	t.Helper()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("turn directory %q survived delivery: %v", dir, err)
	}
}

func TestReplyFilesFallBackToTextWithoutFileSender(t *testing.T) {
	root := t.TempDir()
	frame := turnFrame(t, root)
	frame.Refused = []string{"big.bin"}
	gw := &textRecordingGateway{}
	d := newSessionDriver("s1", []contracts.GatewaySet{{Gateway: gw}}, nil, nil)
	d.outboxRoot = root

	d.deliverReply(context.Background(), contracts.Event{T: "reply", Text: "hello", Done: true}, frame)

	if len(gw.sent) != 1 {
		t.Fatalf("gateway got %d messages, want one fallback message: %q", len(gw.sent), gw.sent)
	}
	for _, want := range []string{"hello", "a.png", "big.bin"} {
		if !strings.Contains(gw.sent[0], want) {
			t.Fatalf("fallback text %q lacks %q", gw.sent[0], want)
		}
	}
	if strings.Count(gw.sent[0], "hello") != 1 {
		t.Fatalf("reply text duplicated in %q", gw.sent[0])
	}
	assertTurnDirRemoved(t, frame.Dir)
}

func TestReplyFilesSentOnceThroughFileSender(t *testing.T) {
	root := t.TempDir()
	frame := turnFrame(t, root)
	gw := &fileRecordingGateway{}
	d := newSessionDriver("s1", []contracts.GatewaySet{{Gateway: gw}}, nil, nil)
	d.outboxRoot = root

	d.deliverReply(context.Background(), contracts.Event{T: "reply", Text: "hello", Done: true}, frame)

	if len(gw.sentText) != 1 || gw.sentText[0] != "hello" {
		t.Fatalf("SendFiles texts = %q, want one call with the reply text only", gw.sentText)
	}
	if len(gw.sentFiles) != 1 || gw.sentFiles[0] != "a.png" {
		t.Fatalf("SendFiles files = %q, want [a.png]", gw.sentFiles)
	}
	if len(gw.sent) != 0 {
		t.Fatalf("gateway also got plain messages %q, the text would be duplicated", gw.sent)
	}
	assertTurnDirRemoved(t, frame.Dir)
}
