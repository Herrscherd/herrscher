package host

import (
	"context"
	"sync"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/checkpoint"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/otelexport"
)

type sessionTable struct {
	mu          sync.Mutex
	m           map[string]*sessionDriver
	messages    *hostMessages
	cliMessages *clitext.Messages
	checkpoints checkpoint.Store
	persists    *persistWrites
	compaction  compactionPolicy
	store       *state.State
	outboxRoot  string

	outboundMaxBytes int64
	fileSenders      map[string]contracts.FileSender
	telemetry        otelexport.Exporter

	tokens     map[string]string
	seedLeases map[string]seedLease

	journal *eventJournal

	notices *sessionNotices
}

func (t *sessionTable) percentOfSession(name string) int {
	if t.store != nil {
		if sess, ok := t.store.FindSession(name); ok {
			return t.compaction.percentFor(sess)
		}
	}
	return t.compaction.percent
}

func newSessionTable(messages *hostMessages, cliMessages *clitext.Messages) *sessionTable {
	return &sessionTable{m: make(map[string]*sessionDriver), messages: messages, cliMessages: cliMessages, persists: newPersistWrites(), journal: newEventJournal(), notices: &sessionNotices{}}
}

func (t *sessionTable) oneShotPersists() *persistWrites {
	return t.persists.child()
}

func (t *sessionTable) register(name string, driver *sessionDriver) {
	t.mu.Lock()
	t.m[name] = driver
	t.mu.Unlock()
}

func (t *sessionTable) unregister(name string, driver *sessionDriver) {
	t.mu.Lock()
	if t.m[name] == driver {
		delete(t.m, name)
		delete(t.tokens, name)
	}
	t.mu.Unlock()
}

func (t *sessionTable) lookup(name string) *sessionDriver {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.m[name]
}

func (t *sessionTable) claimIdle(session string) (func(), bool) {
	driver := t.lookup(session)
	if driver == nil {
		return func() {}, true
	}
	if len(driver.queue) > 0 || !driver.claimTurn() {
		return nil, false
	}
	return driver.releaseClaim, true
}

func (t *sessionTable) pick(session, value string) bool {
	driver := t.lookup(session)
	if driver == nil {
		return false
	}
	return driver.Pick(value)
}

func (t *sessionTable) Interrupt(session string) bool {
	driver := t.lookup(session)
	if driver == nil {
		return false
	}
	driver.Interrupt()
	return true
}

func (t *sessionTable) Seed(session, task string) bool {
	driver := t.lookup(session)
	if driver == nil {
		return false
	}
	return driver.Seed(task)
}

func (t *sessionTable) Submit(ctx context.Context, session string, inbound contracts.Inbound) bool {
	driver := t.lookup(session)
	if driver == nil {
		return false
	}
	return driver.submit(ctx, inbound)
}
