package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/eventwire"
	"github.com/Herrscherd/herrscher/core/host"
)

const (
	watchVerb      = "watch"
	argSince       = "--since"
	watchMoneyDigs = 4
)

var (
	errWatchMissingName = errors.New("session watch needs --name")
	errWatchBadSince    = errors.New("session watch --since needs a seq number")
	errWatchDaemonLost  = errors.New("the daemon stopped answering")
	errWatchOldDaemon   = errors.New("the daemon sends events without a seq")
)

type watchOptions struct {
	name   string
	since  string
	asJSON bool
}

type eventSubscriber func(ctx context.Context, cursor host.EventCursor) (<-chan host.DaemonEvent, error)

func runSessionWatch(ctx context.Context, layout host.RuntimeLayout, messages *clitext.Messages, hostMessages *host.Messages, args []string) error {
	lang := clitext.ClientLanguage()
	opts, unknown := parseWatchArgs(args)
	if unknown != "" {
		return renderError(messages, lang, errUnknownArgument, clitext.FlagUnknown, clitext.Params{clitext.ParamCommand: verbSession + " " + watchVerb, clitext.ParamName: strconv.Quote(unknown)})
	}
	if opts.name == "" {
		return renderError(messages, lang, errWatchMissingName, clitext.HostErrMissingName, nil)
	}
	cursor, err := opts.cursor()
	if err != nil {
		return renderError(messages, lang, errWatchBadSince, clitext.SessionWatchBadSince, clitext.Params{clitext.ParamArg: strconv.Quote(opts.since)})
	}
	instance := host.ServedInstanceID(host.DefaultStatePath(), "")
	subscribe := func(ctx context.Context, cursor host.EventCursor) (<-chan host.DaemonEvent, error) {
		return host.SubscribeSessionEvents(ctx, hostMessages, layout, instance, cursor)
	}
	follow := sessionFollow{view: newWatchView(os.Stdout, messages, lang, opts.asJSON), cursor: cursor}
	return follow.run(ctx, subscribe)
}

func parseWatchArgs(args []string) (watchOptions, string) {
	var opts watchOptions
	for len(args) > 0 {
		arg := args[0]
		args = args[1:]
		key, value, inline := strings.Cut(arg, "=")
		if arg == argJSON {
			opts.asJSON = true
			continue
		}
		if key != argName && key != argSince {
			if opts.name != "" || strings.HasPrefix(arg, "--") {
				return opts, arg
			}
			opts.name = arg
			continue
		}
		if !inline {
			if len(args) == 0 {
				return opts, arg
			}
			value, args = args[0], args[1:]
		}
		opts.set(key, value)
	}
	return opts, ""
}

func (o *watchOptions) set(key, value string) {
	if key == argName {
		o.name = value
		return
	}
	o.since = value
}

func (o *watchOptions) cursor() (host.EventCursor, error) {
	cursor := host.EventCursor{Session: host.SessionSlug(o.name)}
	if o.since == "" {
		return cursor, nil
	}
	after, err := strconv.ParseUint(o.since, 10, 64)
	if err != nil {
		return cursor, err
	}
	cursor.Since = &after
	return cursor, nil
}

type followStep int

const overtakenLimit = 256

const (
	stepContinue followStep = iota
	stepResume
	stepClosed
)

type sessionFollow struct {
	view      *watchView
	cursor    host.EventCursor
	seen      uint64
	overtaken map[uint64]bool
}

func (f *sessionFollow) run(ctx context.Context, subscribe eventSubscriber) error {
	for {
		step, err := f.attach(ctx, subscribe)
		if err != nil || step != stepResume {
			return err
		}
	}
}

func (f *sessionFollow) attach(ctx context.Context, subscribe eventSubscriber) (followStep, error) {
	attached, detach := context.WithCancel(ctx)
	defer detach()
	stream, err := subscribe(attached, f.resumeCursor())
	if err != nil {
		return stepClosed, err
	}
	for {
		select {
		case <-ctx.Done():
			return stepClosed, f.view.finish()
		case event, open := <-stream:
			if !open {
				return stepClosed, f.view.lost(f.seen)
			}
			step, err := f.take(&event)
			if err != nil || step != stepContinue {
				return step, err
			}
		}
	}
}

func (f *sessionFollow) resumeCursor() host.EventCursor {
	if f.seen == 0 {
		return f.cursor
	}
	seen := f.seen
	return host.EventCursor{Session: f.cursor.Session, Since: &seen}
}

func (f *sessionFollow) takeOvertaking(event *host.DaemonEvent) error {
	if event.Seq <= f.seen || f.overtaken[event.Seq] {
		return nil
	}
	if f.overtaken == nil {
		f.overtaken = make(map[uint64]bool)
	}
	if len(f.overtaken) >= overtakenLimit {
		delete(f.overtaken, slices.Min(slices.Collect(maps.Keys(f.overtaken))))
	}
	f.overtaken[event.Seq] = true
	return f.view.show(event)
}

func (f *sessionFollow) take(event *host.DaemonEvent) (followStep, error) {
	if event.T == eventwire.EventGap && event.Value == eventwire.GapDropped {
		return stepResume, nil
	}
	if event.Session != f.cursor.Session {
		return stepContinue, nil
	}
	if event.T == eventwire.EventGap || event.T == eventwire.EventCaughtUp {
		return stepContinue, f.view.show(event)
	}
	if event.Seq == 0 {
		return stepClosed, f.view.refuse(errWatchOldDaemon, clitext.SessionWatchOldDaemon)
	}
	if event.Coordination != nil {
		return stepContinue, f.takeOvertaking(event)
	}
	if event.Seq <= f.seen {
		return stepContinue, nil
	}
	f.seen = event.Seq
	maps.DeleteFunc(f.overtaken, func(seq uint64, _ bool) bool { return seq <= f.seen })
	if event.T != eventwire.EventClosed {
		return stepContinue, f.view.show(event)
	}
	if event.Value == eventwire.ClosedRenamed && event.Text != "" {
		f.cursor.Session = event.Text
		return stepContinue, f.view.show(event)
	}
	return stepClosed, f.view.show(event)
}

type watchView struct {
	out      io.Writer
	messages *clitext.Messages
	lang     i18n.Language
	asJSON   bool
	midLine  bool
	streamed bool
}

func newWatchView(out io.Writer, messages *clitext.Messages, lang i18n.Language, asJSON bool) *watchView {
	return &watchView{out: out, messages: messages, lang: lang, asJSON: asJSON}
}

func (p *watchView) show(event *host.DaemonEvent) error {
	if p.asJSON {
		line, err := json.Marshal(event)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(p.out, "%s\n", line)
		return err
	}
	return p.render(event)
}

func (p *watchView) render(event *host.DaemonEvent) error {
	switch event.T {
	case eventwire.EventHuman:
		p.streamed = false
		return p.line(clitext.SessionWatchHuman, clitext.Params{clitext.ParamName: event.Who, clitext.ParamMessage: event.Text})
	case eventwire.EventChunk:
		p.streamed = true
		p.midLine = !strings.HasSuffix(event.Text, "\n")
		_, err := io.WriteString(p.out, event.Text)
		return err
	case eventwire.EventStatus:
		return p.line(clitext.SessionWatchStatus, clitext.Params{clitext.ParamMessage: event.Text})
	case eventwire.EventSubagent:
		if event.Subagent == nil {
			return nil
		}
		return p.line(clitext.SessionWatchSubagent, clitext.Params{clitext.ParamName: event.Subagent.Name, clitext.ParamState: event.Subagent.State})
	case eventwire.EventNotice, eventwire.EventBackendNotice:
		return p.line(clitext.SessionWatchNotice, clitext.Params{clitext.ParamMessage: event.Text})
	case eventwire.EventReply:
		return p.reply(event)
	case eventwire.EventAbandoned:
		return p.line(clitext.SessionWatchAbandoned, nil)
	case eventwire.EventClosed:
		return p.closed(event)
	case eventwire.EventGap:
		return p.line(clitext.SessionWatchGap, nil)
	case eventwire.EventCaughtUp:
		return p.caughtUp(event)
	}
	return nil
}

func (p *watchView) closed(event *host.DaemonEvent) error {
	if event.Value == eventwire.ClosedRenamed {
		return p.line(clitext.SessionWatchRenamed, clitext.Params{clitext.ParamName: event.Session, clitext.ParamTo: event.Text})
	}
	return p.line(clitext.SessionWatchClosed, clitext.Params{clitext.ParamName: event.Session})
}

func (p *watchView) caughtUp(event *host.DaemonEvent) error {
	switch event.Value {
	case eventwire.CaughtUpIdle:
		return p.line(clitext.SessionWatchIdle, clitext.Params{clitext.ParamName: event.Session})
	case eventwire.CaughtUpEmpty:
		return p.line(clitext.SessionWatchEmpty, clitext.Params{clitext.ParamName: event.Session})
	}
	return nil
}

func (p *watchView) reply(event *host.DaemonEvent) error {
	if !p.streamed && event.Text != "" {
		if err := p.raw(event.Text + "\n"); err != nil {
			return err
		}
	}
	if !event.Done {
		return nil
	}
	p.streamed = false
	return p.line(clitext.SessionWatchDone, clitext.Params{
		clitext.ParamIn:   clitext.FormatNumber(p.lang, float64(event.TokensIn), 0),
		clitext.ParamOut:  clitext.FormatNumber(p.lang, float64(event.Tokens), 0),
		clitext.ParamCost: "$" + clitext.FormatNumber(p.lang, event.Cost, watchMoneyDigs),
	})
}

func (p *watchView) line(key clitext.Key, params clitext.Params) error {
	text, err := p.messages.Text(p.lang, key, params)
	if err != nil {
		return err
	}
	if p.midLine {
		text = "\n" + text
	}
	return p.raw(text + "\n")
}

func (p *watchView) raw(text string) error {
	p.midLine = !strings.HasSuffix(text, "\n")
	_, err := io.WriteString(p.out, text)
	return err
}

func (p *watchView) finish() error {
	if p.midLine && !p.asJSON {
		return p.raw("\n")
	}
	return nil
}

func (p *watchView) lost(seen uint64) error {
	if err := p.finish(); err != nil {
		return err
	}
	return renderError(p.messages, p.lang, errWatchDaemonLost, clitext.SessionWatchDaemonLost, clitext.Params{clitext.ParamSeq: strconv.FormatUint(seen, 10)})
}

func (p *watchView) refuse(cause error, key clitext.Key) error {
	if err := p.finish(); err != nil {
		return err
	}
	return renderError(p.messages, p.lang, cause, key, nil)
}
