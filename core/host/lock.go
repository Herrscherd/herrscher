package host

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var (
	errLockNotOwned = errors.New("the lock file is not owned by this user")
	errLockSymlink  = errors.New("the lock file is a symbolic link")
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
// is not one, so the second daemon is refused here, at the one place that can
// still explain why.
//
// The lock is advisory and held by the process, not the file: it is released by
// the kernel when the daemon exits, however it exits, so a crash never leaves a
// lock nobody can clear. The pid is written for the error message alone; nothing
// reads it to make a decision.
func LockState(statePath string) (func(), error) {
	release, err := takeLock(StateLockPath(statePath))
	if taken, ok := errors.AsType[*lockTakenError](err); ok {
		return nil, &lockHeldError{StatePath: statePath, Holder: taken.Holder}
	}
	return release, err
}

type lockTakenError struct {
	Holder string
}

func (e *lockTakenError) Error() string {
	return "lock held by another process" + e.Holder
}

func takeLock(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), privateDirMode); err != nil {
		return nil, wrapHost(err, "lock.dir", nil)
	}
	lockFile, err := openLockFile(path)
	if err != nil {
		return nil, wrapHost(err, "lock.unsafe", map[string]any{pathParam: path, "Detail": err.Error()})
	}
	locked, err := tryLock(lockFile)
	if err != nil {
		closeRead(lockFile)
		return nil, wrapHost(err, "lock.state", nil)
	}
	if !locked {
		holder := readHolder(lockFile)
		closeRead(lockFile)
		return nil, &lockTakenError{Holder: holder}
	}
	if err := lockFile.Truncate(0); err == nil {
		logWriteFailure(lockFile.WriteAt([]byte(strconv.Itoa(os.Getpid())), 0))
	}
	return func() { closeRead(lockFile) }, nil
}

func readHolder(f *os.File) string {
	pid := readPID(f)
	if pid <= 0 {
		return ""
	}
	return fmt.Sprintf(" (pid %d)", pid)
}

func readPID(file *os.File) int {
	buf := make([]byte, pidReadBytes)
	count, readErr := file.ReadAt(buf, 0)
	logCleanup(readErr)
	pid, err := strconv.Atoi(strings.TrimSpace(string(buf[:count])))
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}
