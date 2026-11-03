package host

import (
	"context"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	transport "github.com/Herrscherd/herrscher-transport"
)

func TestAwaitEntriesWakesWhenAnAnnouncementArrives(t *testing.T) {
	watch := newRemoteWatch(func(string) bool { return true }, func(error) {})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	type outcome struct {
		entries []transport.RemoteEntry
		err     error
	}
	done := make(chan outcome, 1)
	go func() {
		entries, err := awaitEntries(ctx, contracts.CategoryMemory, watch)
		done <- outcome{entries, err}
	}()

	watch.record(transport.Announcement{
		InstanceID: "memory-1",
		GrpcAddr:   "10.0.0.7:7000",
		Manifest:   contracts.Manifest{Kind: "memory", Category: contracts.CategoryMemory},
	})

	got := <-done
	if got.err != nil {
		t.Fatalf("awaitEntries returned %v, want the announced entry", got.err)
	}
	if len(got.entries) != 1 || got.entries[0].InstanceID != "memory-1" {
		t.Fatalf("awaitEntries = %v, want the memory announcement", got.entries)
	}
}

func TestRecordSignalsTheWaitingResolver(t *testing.T) {
	watch := newRemoteWatch(func(string) bool { return true }, func(error) {})
	watch.record(transport.Announcement{
		InstanceID: "memory-1",
		GrpcAddr:   "10.0.0.7:7000",
		Manifest:   contracts.Manifest{Kind: "memory", Category: contracts.CategoryMemory},
	})
	select {
	case <-watch.arrived:
	default:
		t.Fatal("an accepted announcement must wake a resolver waiting for entries")
	}
}
