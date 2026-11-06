package host

import (
	"context"
	"net"
	"sync"

	"github.com/nats-io/nats.go"

	contracts "github.com/Herrscherd/herrscher-contracts"
	transport "github.com/Herrscherd/herrscher-transport"
)

type remoteWatch struct {
	admit      func(addr string) bool
	onRejected func(error)
	arrived    chan struct{}
	mu         sync.Mutex
	entries    map[string]*transport.RemoteEntry
}

func newRemoteWatch(admit func(addr string) bool, onRejected func(error)) *remoteWatch {
	return &remoteWatch{
		admit:      admit,
		onRejected: onRejected,
		arrived:    make(chan struct{}, 1),
		entries:    make(map[string]*transport.RemoteEntry),
	}
}

func (w *remoteWatch) record(announcement transport.Announcement) {
	if err := w.check(announcement); err != nil {
		w.onRejected(err)
		return
	}
	w.mu.Lock()
	entry := transport.RemoteEntry(announcement)
	w.entries[announcement.InstanceID] = &entry
	w.mu.Unlock()
	select {
	case w.arrived <- struct{}{}:
	default:
	}
}

func (w *remoteWatch) check(announcement transport.Announcement) error {
	if announcement.InstanceID == "" || announcement.Manifest.Category == "" {
		return transport.ErrUnusableAnnouncement
	}
	host, port, err := net.SplitHostPort(announcement.GrpcAddr)
	if err != nil || host == "" || port == "" {
		return transport.ErrUnusableAnnouncement
	}
	if !w.admit(announcement.GrpcAddr) {
		return transport.ErrAddrRefused
	}
	return nil
}

func (w *remoteWatch) byCategory(category contracts.Category) []transport.RemoteEntry {
	w.mu.Lock()
	defer w.mu.Unlock()
	var found []transport.RemoteEntry
	for _, entry := range w.entries {
		if entry.Manifest.Category == category {
			found = append(found, *entry)
		}
	}
	return found
}

func awaitEntries(ctx context.Context, category contracts.Category, watch *remoteWatch) ([]transport.RemoteEntry, error) {
	for {
		if found := watch.byCategory(category); len(found) > 0 {
			return found, nil
		}
		select {
		case <-ctx.Done():
			return nil, wrapEnglish(ctx.Err(), "remote.no_remote", map[string]any{"Category": category})
		case <-watch.arrived:
		}
	}
}

func (r *Resolver) watchAnnouncements() (*remoteWatch, func(), error) {
	conn, err := nats.Connect(r.natsURL())
	if err != nil {
		return nil, nil, err
	}
	watch := newRemoteWatch(r.addrAllowed, r.warnRejectedAnnouncement)
	if _, err := transport.WatchAnnouncements(conn, r.announceKey, watch.record, r.warnRejectedAnnouncement); err != nil {
		conn.Close()
		return nil, nil, err
	}
	return watch, conn.Close, nil
}
