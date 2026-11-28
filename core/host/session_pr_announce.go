package host

import (
	"log/slog"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/eventwire"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

type sessionNotices struct {
	publish func(session string, event contracts.Event)
}

func (n *sessionNotices) bind(publish func(session string, event contracts.Event)) {
	if n != nil {
		n.publish = publish
	}
}

func (n *sessionNotices) linked() bool {
	return n != nil && n.publish != nil
}

func (n *sessionNotices) emit(session string, event contracts.Event) {
	if !n.linked() {
		return
	}
	n.publish(session, event)
}

func (p sessionPRs) recordPullRequest(name string, record state.PullRequestRecord) error {
	if err := p.store.RefreshPullRequest(name, record); err != nil {
		return err
	}
	p.announceEnd(name, record)
	return nil
}

func (p sessionPRs) announceEnd(name string, record state.PullRequestRecord) {
	if !record.Ended() || !p.notices.linked() {
		return
	}
	var key clitext.Key = clitext.SessionPREndedClosed
	if record.State == contracts.PullRequestMerged {
		key = clitext.SessionPREndedMerged
	}
	text, err := p.messages.Text(namedSessionLanguage(p.store, name, p.lang()), key, clitext.Params{
		clitext.ParamName:   escapeLine(name),
		clitext.ParamNumber: record.Number,
	})
	if err != nil {
		slog.Warn("session pull request end not announced", "session", name, "err", err)
		return
	}
	claimed, err := p.store.ClaimPullRequestEnd(name)
	if err != nil {
		slog.Warn("session pull request end not recorded", "session", name, "err", err)
		return
	}
	if !claimed {
		return
	}
	p.notices.emit(name, contracts.Event{T: eventwire.EventNotice, Text: text})
}
