package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/host"
	"github.com/Herrscherd/herrscher/plugins/terminal"
	"github.com/Herrscherd/herrscher/plugins/terminal/tui"
)

// A bare `herrscher` serves: it builds the daemon and runs the TUI on top of it.
// That is the right thing when nothing else is running and the wrong thing when
// the service is, because two daemons over one state file answer every message
// twice — so the lock refuses the second one, and the operator is left with a
// running agent they cannot see.
//
// attachedDaemon is the other half of that answer: the same TUI, driven over the
// two sockets the daemon already exposes to other processes. Events come off the
// events socket; everything else is an operator command over the command socket,
// which is the daemon's own dispatch path — so an attached frontend can do
// exactly what an in-process one can, and nothing more.
type attachedDaemon struct {
	ctx      context.Context
	instance string
	events   chan tui.RoutedEvent

	mu sync.Mutex
	// sessions is the last `session info` answer, and channelOf/nameOf index it
	// both ways: an event names its session, the TUI names a channel, and the
	// mapping between them lives only in that answer.
	sessions  []contracts.SessionInfo
	fetched   time.Time
	channelOf map[string]string // session name → channel id
	nameOf    map[string]string // channel id → session name
}

// sessionCacheTTL bounds how stale the tab list may be. The TUI asks for it on
// every repaint, and a socket round trip per keystroke would be felt; a second
// is short enough that a session created in another window shows up right away.
const sessionCacheTTL = time.Second

func newAttachedDaemon(ctx context.Context, instance string) (*attachedDaemon, error) {
	stream, err := host.SubscribeDaemonEvents(ctx, instance)
	if err != nil {
		return nil, err
	}
	a := &attachedDaemon{
		ctx:       ctx,
		instance:  instance,
		events:    make(chan tui.RoutedEvent, 256),
		channelOf: map[string]string{},
		nameOf:    map[string]string{},
	}
	// Prime the table before the first event arrives, so an event for a session
	// the frontend has never asked about still finds its channel.
	a.refresh()
	go a.pump(stream)
	return a, nil
}

// pump tags each daemon event with the conversation its session is bound to and
// hands it to the TUI. The stream closing means the daemon went away; the
// frontend's channel closes with it rather than hanging on a stream that will
// never carry another line.
func (a *attachedDaemon) pump(stream <-chan host.DaemonEvent) {
	defer close(a.events)
	for e := range stream {
		conv := contracts.Conversation{Gateway: "terminal", ID: a.channelFor(e.Session)}
		select {
		case a.events <- tui.RoutedEvent{Conv: conv, Event: e.Event}:
		case <-a.ctx.Done():
			return
		}
	}
}

// channelFor maps a session name to the conversation the TUI tabs by, refreshing
// the table once when the name is unknown — a session created seconds ago is the
// common case, and the alternative is an event rendered into no tab at all.
func (a *attachedDaemon) channelFor(session string) string {
	a.mu.Lock()
	ch, ok := a.channelOf[session]
	a.mu.Unlock()
	if ok {
		return ch
	}
	a.refresh()
	a.mu.Lock()
	defer a.mu.Unlock()
	if ch, ok := a.channelOf[session]; ok {
		return ch
	}
	// Falling back to the session name keeps the event visible in a tab of its
	// own instead of dropping it, which is what a wrong-but-present id costs.
	return session
}

// refresh reloads the session table from the daemon unconditionally.
func (a *attachedDaemon) refresh() {
	out, err := a.dispatch([]string{"session", "info", "--json"})
	if err != nil {
		return // the table stays as it was; the next call tries again
	}
	var infos []contracts.SessionInfo
	if err := json.Unmarshal([]byte(out), &infos); err != nil {
		return
	}
	channelOf := make(map[string]string, len(infos))
	nameOf := make(map[string]string, len(infos))
	for _, s := range infos {
		channelOf[s.Name] = s.ChannelID
		nameOf[s.ChannelID] = s.Name
	}
	a.mu.Lock()
	a.sessions, a.channelOf, a.nameOf, a.fetched = infos, channelOf, nameOf, time.Now()
	a.mu.Unlock()
}

func (a *attachedDaemon) dispatch(argv []string) (string, error) {
	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
	defer cancel()
	return host.DaemonDispatch(ctx, a.instance, argv)
}

// runAttached opens the TUI against a daemon this process does not own. Quitting
// it returns; it never cancels anything on the other side, because the daemon
// was there first and outlives this window — closing a viewer must not close the
// agent it was viewing.
func runAttached(ctx context.Context, instance string, opts ...tui.Option) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	a, err := newAttachedDaemon(ctx, instance)
	if err != nil {
		return err
	}
	return tui.Run(ctx, cancel, a, opts...)
}

// --- tui.Backend ---

func (a *attachedDaemon) Frontend() <-chan tui.RoutedEvent { return a.events }

func (a *attachedDaemon) Sessions() []contracts.SessionInfo {
	a.mu.Lock()
	fresh := time.Since(a.fetched) < sessionCacheTTL
	sessions := a.sessions
	a.mu.Unlock()
	if fresh {
		return sessions
	}
	a.refresh()
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sessions
}

// Submit sends the typed line to the session the tab belongs to. Attachments
// travel as local paths: the frontend and the daemon share a filesystem, and the
// daemon pins every one of them to its staging root before reading.
func (a *attachedDaemon) Submit(channel, text string, attachments []tui.Attachment) {
	a.mu.Lock()
	name, ok := a.nameOf[channel]
	a.mu.Unlock()
	if !ok {
		a.refresh()
		a.mu.Lock()
		name, ok = a.nameOf[channel]
		a.mu.Unlock()
	}
	if !ok {
		a.notify(channel, "no session is bound to this tab any more")
		return
	}
	argv := []string{"session", "send", "--name", name, "--text", text}
	if len(attachments) > 0 {
		paths := make([]string, 0, len(attachments))
		for _, at := range attachments {
			paths = append(paths, at.Path)
		}
		b, err := json.Marshal(paths)
		if err == nil {
			argv = append(argv, "--attachments", string(b))
		}
	}
	if _, err := a.dispatch(argv); err != nil {
		a.notify(channel, err.Error())
	}
}

// notify surfaces a client-side failure in the tab it happened in. The daemon
// never saw it, so it will never come back over the events socket, and silence
// would read as a message that was sent and ignored.
func (a *attachedDaemon) notify(channel, text string) {
	select {
	case a.events <- tui.RoutedEvent{
		Conv:  contracts.Conversation{Gateway: "terminal", ID: channel},
		Event: contracts.Event{T: "status", Text: "herrscher: " + text},
	}:
	default:
	}
}

func (a *attachedDaemon) Dispatch(args []string) (string, error) {
	if len(args) == 0 || !attachedVerbs[args[0]] {
		verb := ""
		if len(args) > 0 {
			verb = args[0]
		}
		return "", fmt.Errorf("command %q is not available from the terminal (allowed: session, agent)", verb)
	}
	return a.dispatch(withTerminalOnly(args))
}

// attachedVerbs mirrors the terminal gateway's own allow-list: an attached
// frontend reaches the daemon through the same door, so it must not open onto
// more than the in-process one does. Service and config verbs would let a tab
// restart or re-point the very daemon it is attached to.
var attachedVerbs = map[string]bool{"session": true, "agent": true}

// withTerminalOnly makes a session created from a tab bind to the terminal
// gateway, the way the in-process TUI does, so it comes back as a tab rather
// than as a session with no visible home.
func withTerminalOnly(args []string) []string {
	if len(args) < 2 || args[0] != "session" || args[1] != "create" {
		return args
	}
	for _, a := range args {
		if a == "--gateways" || a == "--terminal_only" {
			return args
		}
	}
	return append(append([]string(nil), args...), "--terminal_only")
}

func (a *attachedDaemon) Close(name string, force bool) (string, error) {
	argv := []string{"session", "close", "--name", name}
	if force {
		argv = append(argv, "--force")
	}
	return a.dispatch(argv)
}

func (a *attachedDaemon) Resume(name string) (string, error) {
	return a.dispatch([]string{"session", "resume", "--name", name})
}

func (a *attachedDaemon) Interrupt(name string) bool {
	_, err := a.dispatch([]string{"session", "interrupt", "--name", name})
	return err == nil
}

func (a *attachedDaemon) Scrollback(name string) []contracts.ScrollbackLine {
	out, err := a.dispatch([]string{"session", "scrollback", "--name", name})
	if err != nil {
		return nil
	}
	var lines []contracts.ScrollbackLine
	if err := json.Unmarshal([]byte(out), &lines); err != nil {
		return nil
	}
	return lines
}

// Commands asks the daemon what it dispatches and keeps the verbs a terminal
// may run. The in-process frontend answers the same question the same way, so
// attaching to a daemon and being one show the same palette — which is the whole
// promise of the attached window.
func (a *attachedDaemon) Commands() []tui.CommandSpec {
	out, err := a.dispatch([]string{"commands", "--json"})
	if err != nil {
		return nil
	}
	specs, err := tui.ParseCommandSpecs([]byte(out), terminal.OperatorVerb)
	if err != nil {
		return nil
	}
	return specs
}
