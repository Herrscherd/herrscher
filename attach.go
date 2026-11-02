package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/host"
	"github.com/Herrscherd/herrscher/plugins/terminal"
	"github.com/Herrscherd/herrscher/plugins/terminal/tui"
)

var errNotAvailableFromTerminal = errors.New("not available from the terminal")

type attachedDaemon struct {
	done     <-chan struct{}
	send     func(argv []string) (string, error)
	instance string
	events   chan tui.RoutedEvent

	mu        sync.Mutex
	sessions  []contracts.SessionInfo
	fetched   time.Time
	channelOf map[string]string
	nameOf    map[string]string
}

const sessionCacheTTL = time.Second

func newAttachedDaemon(ctx context.Context, instance string) (*attachedDaemon, error) {
	stream, err := host.SubscribeDaemonEvents(ctx, instance)
	if err != nil {
		return nil, err
	}
	a := &attachedDaemon{
		done:      ctx.Done(),
		instance:  instance,
		events:    make(chan tui.RoutedEvent, attachBuffer),
		channelOf: map[string]string{},
		nameOf:    map[string]string{},
	}
	a.send = func(argv []string) (string, error) {
		call, cancel := context.WithTimeout(ctx, attachTimeout)
		defer cancel()
		return host.DaemonDispatch(call, instance, argv)
	}
	a.refresh()
	go a.pump(stream)
	return a, nil
}

func (a *attachedDaemon) pump(stream <-chan host.DaemonEvent) {
	defer close(a.events)
	for e := range stream {
		conv := contracts.Conversation{Gateway: kindTerminal, ID: a.channelFor(e.Session)}
		select {
		case a.events <- tui.RoutedEvent{Conv: conv, Event: e.Event}:
		case <-a.done:
			return
		}
	}
}

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
	return session
}

func (a *attachedDaemon) refresh() {
	out, err := a.dispatch([]string{verbSession, "info", argJSON})
	if err != nil {
		return
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
	return a.send(argv)
}

func runAttached(ctx context.Context, instance string, opts ...tui.Option) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	a, err := newAttachedDaemon(ctx, instance)
	if err != nil {
		return err
	}
	return tui.Run(ctx, cancel, a, opts...)
}

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
	argv := append([]string{verbSession, "send", argName, name, "--origin", terminal.Kind}, cli.FlagArg("text", text)...)
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

func (a *attachedDaemon) notify(channel, text string) {
	select {
	case a.events <- tui.RoutedEvent{
		Conv:  contracts.Conversation{Gateway: kindTerminal, ID: channel},
		Event: contracts.Event{T: cmdStatus, Text: "herrscher: " + text},
	}:
	default:
	}
}

func (a *attachedDaemon) Dispatch(args []string) (string, error) {
	if len(args) == 0 || !attachedVerbs()[args[0]] {
		verb := ""
		if len(args) > 0 {
			verb = args[0]
		}
		return "", fmt.Errorf("command %q is %w (allowed: session, agent)", verb, errNotAvailableFromTerminal)
	}
	return a.dispatch(withTerminalOnly(args))
}

func attachedVerbs() map[string]bool {
	return map[string]bool{verbSession: true, "agent": true}
}

func withTerminalOnly(args []string) []string {
	if len(args) < 2 || args[0] != verbSession || args[1] != "create" {
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
	argv := []string{verbSession, "close", argName, name}
	if force {
		argv = append(argv, "--force")
	}
	return a.dispatch(argv)
}

func (a *attachedDaemon) Resume(name string) (string, error) {
	return a.dispatch([]string{verbSession, "resume", argName, name})
}

func (a *attachedDaemon) Interrupt(name string) bool {
	_, err := a.dispatch([]string{verbSession, "interrupt", argName, name})
	return err == nil
}

func (a *attachedDaemon) Scrollback(name string) []contracts.ScrollbackLine {
	out, err := a.dispatch([]string{verbSession, "scrollback", argName, name})
	if err != nil {
		return nil
	}
	var lines []contracts.ScrollbackLine
	if err := json.Unmarshal([]byte(out), &lines); err != nil {
		return nil
	}
	return lines
}

func (a *attachedDaemon) Commands() []tui.CommandSpec {
	out, err := a.dispatch([]string{"commands", argJSON})
	if err != nil {
		return nil
	}
	specs, err := tui.ParseCommandSpecs([]byte(out), terminal.OperatorVerb)
	if err != nil {
		return nil
	}
	return specs
}
