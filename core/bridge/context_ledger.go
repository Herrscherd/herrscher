package bridge

import (
	"crypto/sha256"
	"strings"
	"sync"
	"unicode"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const compactFloorTokens = 100_000

type blockDigest [sha256.Size]byte

type contextLedger struct {
	mu          sync.Mutex
	enabled     bool
	generation  uint64
	incarnation string
	resume      string
	sent        map[contracts.ContextPart]blockDigest
	inputSize   int
}

type contextPlan struct {
	track       bool
	generation  uint64
	incarnation string
	resume      string
	digests     map[contracts.ContextPart]blockDigest
}

func newContextLedger(enabled bool) *contextLedger {
	return &contextLedger{enabled: enabled}
}

func digestOf(text string) blockDigest {
	return sha256.Sum256([]byte(text))
}

func keepsHistory(backend contracts.Backend) bool {
	_, ok := resumeAwareOf(backend)
	return ok
}

func isSlashTurn(text string) bool {
	return strings.HasPrefix(strings.TrimLeftFunc(text, unicode.IsSpace), "/")
}

func (l *contextLedger) plan(incarnation string, keeps bool, resume, text string, turn turnContext) (turnContext, contextPlan) {
	if l == nil || !l.enabled {
		return turn, contextPlan{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if isSlashTurn(text) {
		l.resetLocked()
		return turn, contextPlan{}
	}
	if !keeps || incarnation == "" {
		return turn, contextPlan{}
	}
	if incarnation != l.incarnation || resume != l.resume || l.inputSize >= compactFloorTokens {
		l.resetLocked()
	}
	digests := make(map[contracts.ContextPart]blockDigest, len(turn.blocks))
	for i := range turn.blocks {
		block := &turn.blocks[i]
		digest := digestOf(block.text)
		digests[block.part] = digest
		if known, ok := l.sent[block.part]; ok && known == digest {
			block.omitted = true
		}
	}
	return turn, contextPlan{track: true, generation: l.generation, incarnation: incarnation, resume: resume, digests: digests}
}

func (l *contextLedger) observeInput(size int) {
	if l == nil || !l.enabled || size <= 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.inputSize > 0 && size < l.inputSize/2 {
		l.resetLocked()
	}
	l.inputSize = size
}

func (l *contextLedger) commit(plan contextPlan, resumeAfter string) {
	if l == nil || !plan.track {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if plan.generation != l.generation {
		return
	}
	if resumeAfter == "" || (plan.resume != "" && plan.resume != resumeAfter) {
		l.resetLocked()
		return
	}
	l.incarnation, l.resume, l.sent = plan.incarnation, resumeAfter, plan.digests
}

func (l *contextLedger) reset() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.resetLocked()
}

func (l *contextLedger) resetLocked() {
	l.generation++
	l.incarnation, l.resume, l.sent, l.inputSize = "", "", nil, 0
}
