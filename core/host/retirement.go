package host

import "context"

type retirement struct {
	name        string
	live        liveSession
	haltOverran bool
}

type retiredBatch struct {
	window  teardownWindow
	cancel  context.CancelFunc
	retired []retirement
}

func (h *hub) newBatch() retiredBatch {
	window, cancel := h.newGrace()
	return retiredBatch{window: window, cancel: cancel}
}

func (b retiredBatch) await(owner *hub) {
	defer b.cancel()
	settled := owner.awaitInFlightWithin(b.window, &owner.acceptFails)
	window, cancel := owner.newGrace()
	defer cancel()
	owner.awaitRetired(window, b.retired)
	if settled {
		owner.flushWarningsWithin(window)
	}
}

func (h *hub) awaitRetired(window teardownWindow, retired []retirement) {
	for _, retiree := range retired {
		if retiree.haltOverran {
			continue
		}
		h.awaitTeardown(retiree.name, retiree.live, window)
	}
}
