package host

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

// DaemonEvent is one line off the events socket: a turn event tagged with the
// session it belongs to, since one socket carries every session's stream.
type DaemonEvent struct {
	Session string `json:"session"`
	contracts.Event
}

// ServedInstanceID reports the instance id the daemon serving statePath actually
// namespaces its sockets with, so a client can find them. The daemon freezes that
// id into the state file at boot, resolving it from the owner when no --instance
// was passed, and staying legacy (empty) when pre-existing sessions would be
// orphaned. A client that only knows its own flag therefore guesses wrong every
// time the two disagree, and dials a socket path nothing listens on.
//
// The stored id wins; optID is only the fallback for a state file that cannot be
// read (no daemon has booted here yet, in which case nothing is listening either).
func ServedInstanceID(statePath, optID string) string {
	st, err := state.LoadState(statePath)
	if err != nil {
		return optID
	}
	if st.InstanceID != "" {
		return st.InstanceID
	}
	return optID
}

// DaemonDispatch runs one operator command inside the running daemon and returns
// its output. It reports an error when no daemon is listening, unlike the
// operator CLI's own path, which falls back to running the command locally: a
// client that meant to speak to the daemon must hear that it could not, rather
// than silently acting on a second copy of the state.
func DaemonDispatch(ctx context.Context, instanceID string, argv []string) (string, error) {
	out, handled, err := dispatchLiveCommand(ctx, CommandSocketPath(instanceID), argv)
	if !handled {
		return "", &daemonNotRunningError{Socket: CommandSocketPath(instanceID)}
	}
	return out, err
}

// ForwardToDaemon hands a raw argv to the daemon serving statePath and returns
// what it answered, without interpreting either end. Plugin-contributed verbs
// are namespaced into the registry by live gateway instances, so they exist in
// the daemon and nowhere else: a short-lived process that meets one can only
// relay it.
//
// handled is dispatchLiveCommand's distinction, kept rather than folded into an
// error the way DaemonDispatch folds it: false means the dial missed, so the
// caller is free to answer as it did before there was a daemon to ask. A verb
// that lives only inside a daemon must not claim to exist when none is running.
func ForwardToDaemon(ctx context.Context, statePath, optID string, argv []string) (string, bool, error) {
	return dispatchLiveCommand(ctx, commandSocketTarget(ServedInstanceID(statePath, optID)), argv)
}

// SubscribeDaemonEvents dials the daemon's events socket and streams what it
// publishes until ctx ends or the daemon goes away. The channel is closed on
// either, so a frontend can tell "the daemon stopped" from "I stopped".
//
// The daemon drops a subscriber's lines rather than block a turn on it, so a
// consumer that falls far enough behind loses telemetry. Read promptly.
func SubscribeDaemonEvents(ctx context.Context, instanceID string) (<-chan DaemonEvent, error) {
	return subscribeAt(ctx, EventsSocketPath(instanceID))
}

func subscribeAt(ctx context.Context, path string) (<-chan DaemonEvent, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		return nil, fmt.Errorf("events socket %s: %w", path, err)
	}
	out := make(chan DaemonEvent, 256)
	go func() {
		<-ctx.Done()
		logCleanup(conn.Close())
	}()
	go func() {
		defer close(out)
		defer closeRead(conn)
		sc := bufio.NewScanner(conn)
		sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
		for sc.Scan() {
			var e DaemonEvent
			if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
				continue
			}
			select {
			case out <- e:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

func parseAttachmentPaths(raw string) ([]contracts.Attachment, error) {
	if raw == "" {
		return nil, nil
	}
	var paths []string
	if err := json.Unmarshal([]byte(raw), &paths); err != nil {
		return nil, fmt.Errorf("attachments: expected a JSON array of paths: %w", err)
	}
	out := make([]contracts.Attachment, 0, len(paths))
	for _, p := range paths {
		out = append(out, contracts.Attachment{
			Filename: filepath.Base(p),
			URL:      (&url.URL{Scheme: "file", Path: p}).String(),
		})
	}
	return out, nil
}

// ServingPID reports the pid of the daemon currently serving statePath, and
// false when nobody is. It answers the question the lock error raises, "which
// process has it, and is it still alive?" It does so without claiming the lock. The check
// takes the lock only long enough to find out it was free, then drops it.
func ServingPID(statePath string) (int, bool) {
	f, err := os.OpenFile(StateLockPath(statePath), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return 0, false
	}
	defer closeRead(f)
	locked, err := tryLock(f)
	if err != nil || locked {
		return 0, false
	}
	return readPID(f), true
}
