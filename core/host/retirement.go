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
	owner.awaitRetired(b.window, b.retired)
}

func (h *hub) awaitRetired(window teardownWindow, retired []retirement) {
	for _, retiree := range retired {
		if retiree.haltOverran {
			continue
		}
		h.awaitTeardown(retiree.name, retiree.live, window)
	}
}
