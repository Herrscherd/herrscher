package host

import (
	"bytes"
	"sync"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

const lockWaitGrace = 1200 * time.Millisecond

func TestRetirementWaitingForTheLockKeepsItsGrace(t *testing.T) {
	cases := map[string]func(t *testing.T, h *hub) (op func(), joined func() <-chan struct{}){
		"goDead":        deadLiveSessionCase,
		"reconcile":     reconcileArchivedSessionCase,
		"goLiveStarted": startedSessionCase,
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			var warnings bytes.Buffer
			h := newTestHub(t, withWarnings(&warnings))
			h.teardownGrace = lockWaitGrace
			op, joined := setup(t, h)

			h.lifecycle.Lock()
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				op()
			}()
			time.Sleep(lockWaitGrace + 300*time.Millisecond)
			h.lifecycle.Unlock()
			<-finished

			if warnings.Len() != 0 {
				t.Fatalf("a retirement that waited for the lock wrote warnings although its session stopped in time:\n%s", warnings.String())
			}
			select {
			case <-joined():
			default:
				t.Fatal("the session goroutine was still running when the operation returned: the retirement was not joined")
			}
		})
	}
}

func fakeLiveSession(h *hub, name string) <-chan struct{} {
	exit := make(chan struct{})
	done := make(chan struct{})
	go func() {
		<-exit
		time.Sleep(sessionExitHold)
		close(done)
	}()
	h.live[name] = liveSession{cancel: sync.OnceFunc(func() { close(exit) }), done: done, release: func() {}}
	return done
}

func deadLiveSessionCase(t *testing.T, h *hub) (func(), func() <-chan struct{}) {
	t.Helper()
	const name = "dead"
	done := fakeLiveSession(h, name)
	return func() { h.goDead(name) }, func() <-chan struct{} { return done }
}

func reconcileArchivedSessionCase(t *testing.T, h *hub) (func(), func() <-chan struct{}) {
	t.Helper()
	const name = "archived"
	if err := h.st.AddSession(state.Session{Name: name, ChannelID: "c1", Incarnation: name}); err != nil {
		t.Fatal(err)
	}
	if err := h.st.SetArchived(name, true); err != nil {
		t.Fatal(err)
	}
	done := fakeLiveSession(h, name)
	return func() {
			batch := h.reconcile(t.Context())
			batch.await(h)
		}, func() <-chan struct{} {
			return done
		}
}

func startedSessionCase(t *testing.T, h *hub) (func(), func() <-chan struct{}) {
	t.Helper()
	const name = "started"
	var joinedDone <-chan struct{}
	h.startBridge = func(sess state.Session, _ time.Duration) error {
		holdSessionExit(t, h.sessions, name)
		h.mu.Lock()
		joinedDone = h.live[sess.Name].done
		h.mu.Unlock()
		return errBridgeRefusedStart
	}
	sess := state.Session{Name: name, ChannelID: "c1", Incarnation: name}
	return func() {
			_, err := h.goLiveAndPersist(t.Context(), sess, func() error { return nil })
			if err == nil {
				t.Error("goLiveAndPersist returned no error, want the refused start")
			}
		}, func() <-chan struct{} {
			return joinedDone
		}
}
