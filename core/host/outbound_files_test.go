package host

import (
	"context"
	"errors"
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
	sendErr   error
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
	return "", g.sendErr
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

func framesEvent(t *testing.T, frame outbox.Frame) contracts.Event {
	t.Helper()
	value, err := outbox.Encode(frame)
	if err != nil {
		t.Fatal(err)
	}
	return contracts.Event{T: outbox.EventFiles, Value: value}
}

func newOutboundDriver(gateways []contracts.Gateway, root string) *sessionDriver {
	gws := make([]contracts.GatewaySet, 0, len(gateways))
	for _, gateway := range gateways {
		gws = append(gws, contracts.GatewaySet{Gateway: contracts.Degrade(gateway)})
	}
	d := newSessionDriver("s1", gws, nil, nil)
	d.outboxRoot = root
	d.outboundMaxBytes = outboundTestMaxBytes
	d.fileSenders = fileSendersOf(gateways)
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
	d := newOutboundDriver([]contracts.Gateway{gw}, root)

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
	d := newOutboundDriver([]contracts.Gateway{gw}, root)

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
	d := newOutboundDriver([]contracts.Gateway{gw}, root)

	d.deliverReply(context.Background(), contracts.Event{T: "reply", Text: "hello", Done: true}, frame)

	if len(gw.sentFiles) != 0 {
		t.Fatalf("SendFiles got %q, want the file swapped for a link refused", gw.sentFiles)
	}
	if !strings.Contains(gw.routed[0].Text, "a.png") {
		t.Fatalf("reply text %q lacks the notice of the refused file", gw.routed[0].Text)
	}
}

func TestFailedSendFilesNoticesTheNamesWithoutPaths(t *testing.T) {
	root := t.TempDir()
	frame := turnFrame(t, root)
	gw := &routedFileGateway{sendErr: errors.New("gateway down")}
	d := newOutboundDriver([]contracts.Gateway{gw}, root)

	d.deliverReply(context.Background(), contracts.Event{T: "reply", Text: "hello", Done: true}, frame)

	last := gw.routed[len(gw.routed)-1]
	if last.T != eventStatus || !strings.Contains(last.Text, "a.png") {
		t.Fatalf("last routed event %+v, want a status notice naming a.png", last)
	}
	if strings.Contains(last.Text, frame.Dir) {
		t.Fatalf("status notice %q leaks the host path of the outbox", last.Text)
	}
}

func TestTurnFilesOutsideTheOutboxAreDropped(t *testing.T) {
	root := t.TempDir()
	foreign := turnFrame(t, t.TempDir())
	d := newOutboundDriver(nil, root)

	got := d.decodeTurnFiles(framesEvent(t, foreign))

	if got.Dir != "" || len(got.Files) != 0 {
		t.Fatalf("decodeTurnFiles = %+v, want a frame outside the outbox dropped", got)
	}
	if _, err := os.Stat(foreign.Dir); err != nil {
		t.Fatalf("directory outside the outbox was touched: %v", err)
	}
}

func TestSecondTurnDirReplacesTheFirstAndRemovesIt(t *testing.T) {
	root := t.TempDir()
	first := turnFrame(t, root)
	second := turnFrame(t, root)
	d := newOutboundDriver(nil, root)

	got := d.replaceTurnFiles(first, framesEvent(t, second))

	if got.Dir != second.Dir {
		t.Fatalf("replaceTurnFiles kept %q, want the later turn directory %q", got.Dir, second.Dir)
	}
	assertTurnDirRemoved(t, first.Dir)
}

func TestFramesOfOneTurnDirMerge(t *testing.T) {
	root := t.TempDir()
	first := turnFrame(t, root)
	second := outbox.Frame{
		Dir:     first.Dir,
		Files:   []contracts.OutboundFile{{Path: filepath.Join(first.Dir, "b.png"), Name: "b.png"}},
		Refused: []string{"big.bin"},
	}
	d := newOutboundDriver(nil, root)

	got := d.replaceTurnFiles(first, framesEvent(t, second))

	if got.Dir != first.Dir || len(got.Files) != 2 || !slices.Equal(got.Refused, []string{"big.bin"}) {
		t.Fatalf("replaceTurnFiles = %+v, want both frames of the turn merged", got)
	}
}

func TestUnnamedFileIsLabelledInNotices(t *testing.T) {
	d := newOutboundDriver(nil, t.TempDir())

	got := d.noticeNames([]string{"", "a.png"})

	if want := d.hostText("outbound.unnamed", nil) + ", a.png"; got != want {
		t.Fatalf("noticeNames = %q, want %q", got, want)
	}
}

func TestRemoveTurnFilesDeletesTheTurnDirectory(t *testing.T) {
	root := t.TempDir()
	frame := turnFrame(t, root)
	d := newOutboundDriver(nil, root)

	d.removeTurnFiles(frame)

	assertTurnDirRemoved(t, frame.Dir)
}

func TestReplySaidElsewhereNamesTheFilesThatStayUnsent(t *testing.T) {
	root := t.TempDir()
	frame := turnFrame(t, root)
	gw := &routedFileGateway{}
	d := newOutboundDriver([]contracts.Gateway{gw}, root)
	d.saidElsewhere = true
	var tapped []contracts.Event
	d.emitTap = func(event contracts.Event) { tapped = append(tapped, event) }

	d.deliverReply(context.Background(), contracts.Event{T: "reply", Text: "hello", Done: true}, frame)

	if len(gw.routed) != 0 || len(gw.calls) != 0 || len(gw.sent) != 0 {
		t.Fatalf("gateway got routed=%d calls=%q sent=%q, want nothing for a reply said elsewhere", len(gw.routed), gw.calls, gw.sent)
	}
	if len(tapped) != 1 {
		t.Fatalf("emitTap got %d events, want the reply once", len(tapped))
	}
	text := tapped[0].Text
	if !strings.HasPrefix(text, "hello") || !strings.Contains(text, "a.png") {
		t.Fatalf("reply text %q, want the reply followed by a notice naming a.png", text)
	}
	if strings.Contains(text, frame.Dir) {
		t.Fatalf("reply text %q leaks the host path of the outbox", text)
	}
}

func TestReplySaidElsewhereWithoutFilesAddsNoNotice(t *testing.T) {
	root := t.TempDir()
	d := newOutboundDriver([]contracts.Gateway{&routedFileGateway{}}, root)
	d.saidElsewhere = true
	var tapped []contracts.Event
	d.emitTap = func(event contracts.Event) { tapped = append(tapped, event) }

	d.deliverReply(context.Background(), contracts.Event{T: "reply", Text: "hello", Done: true}, outbox.Frame{})

	if len(tapped) != 1 || tapped[0].Text != "hello" {
		t.Fatalf("emitTap got %+v, want the unchanged reply", tapped)
	}
}
