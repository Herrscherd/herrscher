package host

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
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
	contracts.Event

	Session string `json:"session"`
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
	hostState, err := state.LoadState(statePath)
	if err != nil {
		return optID
	}
	if hostState.InstanceID != "" {
		return hostState.InstanceID
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

// IsDaemonNotRunning reports whether DaemonDispatch failed because no daemon listens.
func IsDaemonNotRunning(err error) bool {
	_, ok := errors.AsType[*daemonNotRunningError](err)
	return ok
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

var errDaemonEventsUnreachable = errors.New("the daemon does not answer on its events socket")

func subscribeAt(ctx context.Context, path string) (<-chan DaemonEvent, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		return nil, wrapHost(errDaemonEventsUnreachable, "daemon.events_socket", map[string]any{pathParam: path})
	}
	out := make(chan DaemonEvent, daemonEventBuffer)
	stopClose := closeOnDone(ctx, conn)
	go func() {
		defer close(out)
		defer stopClose()
		defer closeRead(conn)
		scanner := bufio.NewScanner(conn)
		scanner.Buffer(make([]byte, 0, daemonLineBufferInitial), daemonLineBufferMax)
		for scanner.Scan() {
			var event DaemonEvent
			if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
				continue
			}
			select {
			case out <- event:
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
		return nil, wrapHost(err, "daemon.attachments", nil)
	}
	out := make([]contracts.Attachment, 0, len(paths))
	for _, path := range paths {
		out = append(out, contracts.Attachment{
			Filename: filepath.Base(path),
			URL:      (&url.URL{Scheme: "file", Path: path}).String(),
		})
	}
	return out, nil
}

// ServingPID reports the pid of the daemon currently serving statePath, and
// false when nobody is. It answers the question the lock error raises, "which
// process has it, and is it still alive?" It does so without claiming the lock. The check
// takes the lock only long enough to find out it was free, then drops it.
func ServingPID(statePath string) (int, bool) {
	lockFile, err := os.OpenFile(StateLockPath(statePath), os.O_CREATE|os.O_RDWR, ownerOnlyFileMode)
	if err != nil {
		return 0, false
	}
	defer closeRead(lockFile)
	locked, err := tryLock(lockFile)
	if err != nil || locked {
		return 0, false
	}
	return readPID(lockFile), true
}
