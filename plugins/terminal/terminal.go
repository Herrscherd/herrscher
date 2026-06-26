// Package terminal is the terminal gateway plugin: a chat gateway whose
// "channel" is the local TUI. It self-registers like any gateway (init →
// contracts.Register) and implements Gateway + ChannelReader + EventSink so the
// daemon hub drives it exactly like Discord — polling Read for typed lines and
// fanning the live event stream to it via Emit. The TUI binds to the active
// instance through Active().
package terminal

import (
	"context"
	"strconv"
	"sync"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/plugins/terminal/tui"
)

// ChannelID is the fixed conversation id the terminal gateway uses (single
// local channel).
const ChannelID = "terminal"

func init() {
	contracts.Register(contracts.Plugin{
		Manifest: contracts.Manifest{
			Kind:         "terminal",
			Category:     contracts.CategoryGateway,
			Capabilities: contracts.Capabilities{Replies: true},
		},
		Gateway: newGatewaySet,
	})
}

func newGatewaySet(ctx context.Context, cfg contracts.PluginConfig) (contracts.GatewaySet, error) {
	tm := New()
	return contracts.GatewaySet{Gateway: tm, Reader: tm}, nil
}

// Terminal is the in-process terminal gateway. Typed lines arrive via Submit and
// are drained by the hub through Read; outbound events (Emit/Post/Reply) are
// forwarded to the TUI on Frontend.
type Terminal struct {
	mu      sync.Mutex
	pending []contracts.Message
	nextID  int
	out     chan contracts.Event
}

var (
	_ contracts.Gateway       = (*Terminal)(nil)
	_ contracts.ChannelReader = (*Terminal)(nil)
	_ contracts.EventSink     = (*Terminal)(nil)
	_ contracts.Foreground    = (*Terminal)(nil)
)

// RunForeground satisfies contracts.Foreground: the terminal gateway owns the
// process's main thread by running its Bubbletea TUI, blocking until the user
// quits (which calls cancel to tear the daemon down) or ctx is cancelled. The
// composition root runs this for the one bound gateway that implements
// Foreground, on an interactive TTY only.
func (t *Terminal) RunForeground(ctx context.Context, cancel context.CancelFunc) error {
	return tui.Run(ctx, cancel, t)
}

// New builds an unbound terminal gateway.
func New() *Terminal {
	return &Terminal{out: make(chan contracts.Event, 64)}
}

// Submit enqueues a line the user typed in the TUI as an inbound message.
func (t *Terminal) Submit(text string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.nextID++
	t.pending = append(t.pending, contracts.Message{
		ID:         "t" + strconv.Itoa(t.nextID),
		ChannelID:  ChannelID,
		Content:    text,
		AuthorID:   "local",
		AuthorName: "you",
	})
}

// Frontend yields outbound events for the TUI to render.
func (t *Terminal) Frontend() <-chan contracts.Event { return t.out }

// --- contracts.ChannelReader ---

func (t *Terminal) Enabled() bool          { return true }
func (t *Terminal) DefaultChannel() string { return ChannelID }

func (t *Terminal) EnsureChannel(context.Context, string, string) (contracts.Channel, error) {
	return contracts.Channel{ID: ChannelID, Name: ChannelID}, nil
}

// Read drains and returns all lines typed since the last Read (the hub polls
// this like any gateway). after/limit are ignored: the terminal has no history.
func (t *Terminal) Read(_ context.Context, _ string, _ int, _ string) ([]contracts.Message, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.pending) == 0 {
		return nil, nil
	}
	out := t.pending
	t.pending = nil
	return out, nil
}

func (t *Terminal) Unreact(context.Context, string, string, string) error { return nil }

func (t *Terminal) UpsertStatusMessage(_ context.Context, _, _, content string) (string, error) {
	t.emit(contracts.Event{T: "status", Text: content})
	return "", nil
}

// --- contracts.Gateway ---

func (t *Terminal) Manifest() contracts.Manifest {
	return contracts.Manifest{Kind: "terminal", Category: contracts.CategoryGateway}
}

func (t *Terminal) Post(_ context.Context, _ contracts.Conversation, text string) (contracts.MessageID, error) {
	t.emit(contracts.Event{T: "reply", Text: text, Done: true})
	return "", nil
}

func (t *Terminal) Reply(_ context.Context, _ contracts.Conversation, _ contracts.MessageID, text string) (contracts.MessageID, error) {
	t.emit(contracts.Event{T: "reply", Text: text, Done: true})
	return "", nil
}

func (t *Terminal) React(context.Context, contracts.Conversation, contracts.MessageID, string) error {
	return nil
}

func (t *Terminal) Menu(_ context.Context, _ contracts.Conversation, _ contracts.MessageID, prompt string, opts []contracts.Choice) error {
	t.emit(contracts.Event{T: "status", Text: prompt})
	return nil
}

// --- contracts.EventSink ---

// Emit forwards a live turn event to the TUI. This is the rich path: when the
// hub sees the terminal gateway implements EventSink it streams every event
// here rather than only posting the final reply.
func (t *Terminal) Emit(e contracts.Event) { t.emit(e) }

func (t *Terminal) emit(e contracts.Event) {
	select {
	case t.out <- e:
	default: // TUI not draining fast enough → drop rather than block the hub
	}
}
