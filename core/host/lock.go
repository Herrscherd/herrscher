package host

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// StateLockPath is the lock a daemon holds for the whole time it serves. It sits
// beside the state file rather than being the state file, so a stale lock can be
// reasoned about without risking the state itself.
func StateLockPath(statePath string) string {
	dir, base := filepath.Split(statePath)
	return filepath.Join(dir, "."+base+".lock")
}

// LockState claims the right to serve for one state file and returns the release.
//
// Two daemons sharing a state file is not a smaller version of one daemon: both
// connect to the same gateways with the same credentials, so every message is
// delivered and answered twice, and both supervise the same sessions down to the
// same control socket, where the second bridge overwrites the first. The symptom
// is a bot that replies twice and bills twice, which reads as a routing bug and
// is not one — so the second daemon is refused here, at the one place that can
// still explain why.
//
// The lock is advisory and held by the process, not the file: it is released by
// the kernel when the daemon exits, however it exits, so a crash never leaves a
// lock nobody can clear. The pid is written for the error message alone; nothing
// reads it to make a decision.
func LockState(statePath string) (func(), error) {
	path := StateLockPath(statePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("state lock directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("state lock: %w", err)
	}
	locked, err := tryLock(f)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("state lock: %w", err)
	}
	if !locked {
		holder := readHolder(f)
		f.Close()
		return nil, fmt.Errorf(
			"another herrscher is already serving %s%s\n"+
				"Only one daemon may serve a state file: a second one answers every message a second time.\n"+
				"Note that a bare `herrscher` in a terminal serves — it does not attach to the running daemon.\n"+
				"Stop the other one, or pass --state to serve a separate world.",
			statePath, holder)
	}
	// Best effort: the pid only improves the message the *next* daemon prints, and
	// failing to record it is not a reason to refuse to start.
	if err := f.Truncate(0); err == nil {
		f.WriteAt([]byte(strconv.Itoa(os.Getpid())), 0)
	}
	return func() { f.Close() }, nil
}

// readHolder renders the pid recorded in the lock as a parenthetical, or "" when
// there is nothing readable there — an unhelpful "(pid 0)" would be worse than
// saying nothing.
func readHolder(f *os.File) string {
	b := make([]byte, 32)
	n, _ := f.ReadAt(b, 0)
	pid, err := strconv.Atoi(strings.TrimSpace(string(b[:n])))
	if err != nil || pid <= 0 {
		return ""
	}
	return fmt.Sprintf(" (pid %d)", pid)
}
