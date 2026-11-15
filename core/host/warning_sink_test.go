package host

import (
	"testing"
	"time"
)

type blockedForever struct {
	release chan struct{}
}

func (b *blockedForever) Write(p []byte) (int, error) {
	<-b.release
	return len(p), nil
}

func TestWarningSinkDoesNotBlockWritersAndCountsDrops(t *testing.T) {
	writer := &blockedForever{release: make(chan struct{})}
	t.Cleanup(func() { close(writer.release) })
	sink := newWarningSink(writer)

	const writes = 1000
	returned := runInBackground(func() {
		for range writes {
			if _, err := sink.Write([]byte("warning\n")); err != nil {
				t.Error(err)
			}
		}
	})
	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("writes blocked behind a stuck writer")
	}

	if dropped := sink.droppedCount(); dropped < writes-warningQueueLimit-1 {
		t.Fatalf("dropped %d of %d writes behind a stuck writer, want at least %d", dropped, writes, writes-warningQueueLimit-1)
	}
}
