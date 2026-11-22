package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
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
	errWatchDaemonLost  = errors.New("the daemon stopped answering")
)

type watchOptions struct {
	name   string
	since  string
	asJSON bool
}

func runSessionWatch(ctx context.Context, layout host.RuntimeLayout, messages *clitext.Messages, hostMessages *host.Messages, args []string) error {
	lang := clitext.ClientLanguage()
	opts, unknown := parseWatchArgs(args)
	if unknown != "" {
		return renderError(messages, lang, errUnknownArgument, clitext.FlagUnknown, clitext.Params{clitext.ParamCommand: verbSession + " " + watchVerb, clitext.ParamName: strconv.Quote(unknown)})
	}
	if opts.name == "" {
		return renderError(messages, lang, errWatchMissingName, clitext.HostErrMissingName, nil)
	}
	instance := host.ServedInstanceID(host.DefaultStatePath(), "")
	stream, err := host.SubscribeDaemonEvents(ctx, hostMessages, layout, instance)
	if err != nil {
		return err
	}
	backlog, err := fetchWatchBacklog(ctx, hostMessages, layout, instance, opts)
	if err != nil {
		return err
	}
	return followSession(ctx, newWatchView(os.Stdout, messages, lang, opts.asJSON), opts.name, backlog, stream)
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

func fetchWatchBacklog(ctx context.Context, hostMessages *host.Messages, layout host.RuntimeLayout, instance string, opts watchOptions) ([]host.DaemonEvent, error) {
	argv := []string{verbSession, watchVerb, argName, opts.name}
	if opts.since != "" {
		argv = append(argv, argSince, opts.since)
	}
	out, err := host.DaemonDispatch(ctx, hostMessages, layout, instance, argv)
	if err != nil {
		return nil, err
	}
	var backlog []host.DaemonEvent
	if err := json.Unmarshal([]byte(out), &backlog); err != nil {
		return nil, err
	}
	return backlog, nil
}

func followSession(ctx context.Context, view *watchView, name string, backlog []host.DaemonEvent, stream <-chan host.DaemonEvent) error {
	follow := sessionFollow{view: view, name: name}
	for idx := range backlog {
		if _, err := follow.take(&backlog[idx]); err != nil {
			return err
		}
	}
	for {
		select {
		case <-ctx.Done():
			return view.finish()
		case event, open := <-stream:
			if !open {
				return view.lost()
			}
			closed, err := follow.take(&event)
			if err != nil {
				return err
			}
			if closed {
				return view.finish()
			}
		}
	}
}

type sessionFollow struct {
	view *watchView
	name string
	seen uint64
}

func (f *sessionFollow) take(event *host.DaemonEvent) (bool, error) {
	if event.Session != f.name || event.Seq != 0 && event.Seq <= f.seen {
		return false, nil
	}
	f.seen = max(f.seen, event.Seq)
	return event.T == eventwire.EventClosed, f.view.show(event)
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
		return p.line(clitext.SessionWatchClosed, clitext.Params{clitext.ParamName: event.Session})
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

func (p *watchView) lost() error {
	if err := p.finish(); err != nil {
		return err
	}
	return renderError(p.messages, p.lang, errWatchDaemonLost, clitext.SessionWatchDaemonLost, nil)
}
