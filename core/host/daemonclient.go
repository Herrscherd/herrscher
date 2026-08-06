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

// The daemon already speaks to other processes through two local sockets: a
// command socket that runs one operator argv, and an events socket that streams
// every session's turn events. Together they are enough to drive a session from
// outside the daemon, which is what an attached frontend needs — it renders the
// stream and speaks back through the command socket, while the daemon keeps
// owning the sessions. This file is the client half of both, exported so the
// composition root can build such a frontend without reaching into the daemon's
// internals.

// DaemonEvent is one line off the events socket: a turn event tagged with the
// session it belongs to, since one socket carries every session's stream.
type DaemonEvent struct {
	Session string `json:"session"`
	contracts.Event
}

// ServedInstanceID reports the instance id the daemon serving statePath actually
// namespaces its sockets with, so a client can find them. The daemon freezes that
// id into the state file at boot — resolving it from the owner when no --instance
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
		return "", fmt.Errorf("no herrscher daemon is listening on %s", CommandSocketPath(instanceID))
	}
	return out, err
}

// SubscribeDaemonEvents dials the daemon's events socket and streams what it
// publishes until ctx ends or the daemon goes away. The channel is closed on
// either, so a frontend can tell "the daemon stopped" from "I stopped".
//
// The daemon drops a subscriber's lines rather than block a turn on it, so a
// consumer that falls far enough behind loses telemetry — read promptly.
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
		_ = conn.Close()
	}()
	go func() {
		defer close(out)
		defer conn.Close()
		sc := bufio.NewScanner(conn)
		// One event is one line, but a reply carrying a long diff is a long line;
		// the default 64 KiB token limit would end the stream mid-session.
		sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
		for sc.Scan() {
			var e DaemonEvent
			if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
				continue // a line we cannot read is not a reason to stop reading
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

// parseAttachmentPaths decodes the JSON array `session send` carries its
// attachments in. A list rather than a repeated flag because the command socket
// speaks argv, and a path may legally contain a comma; JSON is the one encoding
// that survives every filename intact. Each path becomes a file:// attachment,
// which the bridge pins to the staging root before reading.
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
			// Through url.URL so a path with a space, '#' or '?' round-trips: a raw
			// "file://"+p concat would truncate at the first '#'.
			URL: (&url.URL{Scheme: "file", Path: p}).String(),
		})
	}
	return out, nil
}

// ServingPID reports the pid of the daemon currently serving statePath, and
// false when nobody is. It answers the question the lock error raises — "which
// process has it, and is it still alive?" — without claiming the lock: the check
// takes the lock only long enough to find out it was free, then drops it.
func ServingPID(statePath string) (int, bool) {
	f, err := os.OpenFile(StateLockPath(statePath), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	locked, err := tryLock(f)
	if err != nil || locked {
		return 0, false // free (or unknowable): nobody is serving
	}
	return readPID(f), true
}
