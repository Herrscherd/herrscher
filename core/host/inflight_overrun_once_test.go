package host

import (
	"strings"
	"testing"
	"time"
)

func TestHubTeardownOverrunIsAnnouncedOncePerStop(t *testing.T) {
	warnings := &lockedBuffer{}
	h := newInflightHub(t, "overrun-"+newTurnID(), time.Minute, warnings)

	h.warnHubOverrun()
	h.warnHubOverrun()

	if got := strings.Count(warnings.String(), hubOverrunText(h, "")); got != 1 {
		t.Fatalf("the hub teardown overrun was announced %d times, want exactly once: %q", got, warnings.String())
	}
}

func TestOperatorCommandOverrunIsNotAHubTeardownOverrun(t *testing.T) {
	warnings := &lockedBuffer{}
	h := newInflightHub(t, "operator-"+newTurnID(), 200*time.Millisecond, warnings)

	h.warnCommandHandlerOverrun(h.teardownGrace)

	if strings.Contains(warnings.String(), hubOverrunText(h, "")) {
		t.Fatalf("a stuck operator command announced the hub teardown: %q", warnings.String())
	}
	if got := strings.Count(warnings.String(), commandOverrunText(h, h.teardownGrace)); got != 1 {
		t.Fatalf("the stuck command was announced %d times, want exactly once: %q", got, warnings.String())
	}
}
