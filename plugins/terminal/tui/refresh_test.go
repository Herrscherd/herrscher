package tui

import (
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

type silentBackend struct {
	*fakeBackend
	release chan struct{}
}

func (b *silentBackend) Sessions() []contracts.SessionInfo {
	<-b.release
	return nil
}

func TestMuteDaemonDoesNotBlockTheTick(t *testing.T) {
	be := &silentBackend{fakeBackend: &fakeBackend{}, release: make(chan struct{})}
	defer close(be.release)
	m := newModel(be)

	returned := make(chan struct{})
	go func() {
		m.Update(tickMsg{})
		close(returned)
	}()

	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("a tick with a silent daemon blocked the UI thread, the session list must be read off the update loop")
	}
}
