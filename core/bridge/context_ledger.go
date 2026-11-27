package bridge

import (
	"crypto/sha256"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

type blockDigest [sha256.Size]byte

type contextLedger struct {
	enabled     bool
	generation  uint64
	incarnation string
	resume      string
	sent        map[contracts.ContextPart]blockDigest
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

func (l *contextLedger) plan(incarnation string, keeps bool, resume string, turn turnContext) (turnContext, contextPlan) {
	if l == nil || !l.enabled || !keeps || incarnation == "" {
		return turn, contextPlan{}
	}
	if incarnation != l.incarnation || resume != l.resume {
		l.reset()
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

func (l *contextLedger) commit(plan contextPlan, resumeAfter string) {
	if l == nil || !plan.track || plan.generation != l.generation {
		return
	}
	if resumeAfter == "" || (plan.resume != "" && plan.resume != resumeAfter) {
		l.reset()
		return
	}
	l.incarnation, l.resume, l.sent = plan.incarnation, resumeAfter, plan.digests
}

func (l *contextLedger) reset() {
	if l == nil {
		return
	}
	l.generation++
	l.incarnation, l.resume, l.sent = "", "", nil
}
