package tui

import (
	"strings"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/termpaint"
	"github.com/Herrscherd/herrscher/core/timeline"
)

type tab struct {
	channel            string
	label              string
	entries            []entry
	unread             bool
	busy               bool
	interruptRequested bool
	streamed           bool
	streamedIDs        []uint64
	pendingBlank       strings.Builder
	lastCost           float64
	disconnected       bool
	tokens             int
	startedAt          time.Time
	costTotal          float64
	ctxTokens          int
	ctxMeasured        bool
	compaction         compaction
	lastCompact        compaction
	openedAt           time.Time
	nextEntryID        uint64
	todos              []contracts.TodoItem
	agents             []liveAgent
	agentsDone         []liveAgent
	background         []backgroundTask
	unreadCount        int
	finished           bool
	calls              timeline.Recorder
	files              []string
	trail              []liveAgent
	flips              map[uint64]bool
	pendingApproval    bool
	approval           approvalPrompt
	mode               modeID
	modeAsked          bool
	queue              []queuedInput
	shellContext       string
	shellRunning       bool
	turnStart          string
	snapshotting       bool
	queued             []queuedTurn
	history            historyState
	historySeq         uint64
	historyName        string
	pendingEvents      []RoutedEvent
}

type historyState uint8

const (
	historyNone historyState = iota
	historyQueued
	historyRequested
	historyDone
)

func (tb *tab) awaitingHistory() bool {
	return tb.history == historyQueued || tb.history == historyRequested
}

type liveAgent struct {
	contracts.Subagent

	since time.Time
	ended time.Time
}

func (tb *tab) dropAgent(agentID string) {
	if index := tb.agentIndex(agentID); index >= 0 {
		tb.agents = append(tb.agents[:index], tb.agents[index+1:]...)
	}
}

func (tb *tab) clearPanels() {
	tb.agents = nil
	tb.agentsDone = nil
	for _, t := range tb.todos {
		if t.State != stateDone {
			return
		}
	}
	tb.todos = nil
}

const maxTabLines = 5000

func (tb *tab) appendEntry(item entry) {
	item.raw = item.text
	item.text = termpaint.EscapeLines(item.text).String()
	tb.nextEntryID++
	item.id = tb.nextEntryID
	item.at = time.Now()
	if item.role == roleYou {
		tb.trail = nil
	}
	tb.pendingBlank.Reset()
	tb.entries = append(tb.entries, item)
	if len(tb.entries) > maxTabLines {
		tb.entries = append([]entry(nil), tb.entries[len(tb.entries)-maxTabLines:]...)
	}
}

func lastAgentIndex(entries []entry) int {
	last := len(entries) - 1
	for last >= 0 && entries[last].role != roleAgent {
		last--
	}
	return last
}

func (tb *tab) measureContext(event contracts.Event) {
	used := event.TokensIn + event.CacheRead + event.CacheCreate
	tb.measureCompaction(used)
	if used <= 0 {
		return
	}
	if !event.Done {
		tb.ctxTokens, tb.ctxMeasured = used, true
	} else if !tb.ctxMeasured {
		tb.ctxTokens = used
	}
}

func (tb *tab) appendThinking(text string, tokens, before int) {
	if strings.TrimSpace(text) == "" {
		return
	}
	tb.endStream()
	if n := len(tb.entries); n > 0 && tb.entries[n-1].role == roleThinking {
		last := &tb.entries[n-1]
		last.raw += text
		last.text += termpaint.EscapeLines(text).String()
		last.ended = time.Now()
		if tokens > 0 {
			last.tokens = tokens
		}
		return
	}
	tb.appendEntry(entry{role: roleThinking, text: text, tokens: tokens, base: before})
}

func (tb *tab) appendReply(trans translator, event contracts.Event) {
	if event.Text != "" && !tb.streamed {
		if text, isErr := hostError(event.Text); isErr {
			tb.appendEntry(entry{role: roleError, text: text})
		} else {
			tb.appendEntry(entry{role: roleAgent, text: event.Text})
		}
	}
	tb.endStream()
	if event.Cost > 0 {
		tb.lastCost = event.Cost
		tb.costTotal += event.Cost
		tb.appendEntry(entry{role: roleCost, text: formatCost(event.Cost)})
	}
	if event.Done {
		tb.finishCompaction(trans)
		tb.busy = false
		tb.interruptRequested = false
		tb.streamed = false
		tb.ctxMeasured = false
		tb.streamedIDs = nil
		tb.clearPanels()
	}
}

func (tb *tab) applySubagent(sub *contracts.Subagent) {
	if sub == nil {
		return
	}
	tb.recordTrail(*sub)
	now := time.Now()
	index := tb.agentIndex(sub.ID)
	if sub.State == stateDone || sub.State == stateFailed || sub.State == stateError {
		since := now
		if index >= 0 {
			since = tb.agents[index].since
			tb.dropAgent(sub.ID)
		}
		tb.agentsDone = append(tb.agentsDone, liveAgent{Subagent: *sub, since: since, ended: now})
		return
	}
	if index >= 0 {
		tb.agents[index].Subagent = *sub
		return
	}
	tb.agents = append(tb.agents, liveAgent{Subagent: *sub, since: now})
}

func (tb *tab) agentIndex(agentID string) int {
	for index, a := range tb.agents {
		if a.ID == agentID {
			return index
		}
	}
	return -1
}

func (tb *tab) recordTrail(sub contracts.Subagent) {
	now := time.Now()
	for index := range tb.trail {
		if tb.trail[index].ID == sub.ID {
			tb.trail[index].Subagent = sub
			if isTerminalAgent(sub.State) && tb.trail[index].ended.IsZero() {
				tb.trail[index].ended = now
			}
			return
		}
	}
	tb.trail = append(tb.trail, liveAgent{Subagent: sub, since: now})
}

func isTerminalAgent(state string) bool {
	return state == stateDone || state == stateFailed || state == stateTimeout
}
