package vendortest

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func appendLine(path, line string) error {
	file, err := os.OpenFile(filepath.Clean(path), os.O_APPEND|os.O_CREATE|os.O_WRONLY, privateFile)
	if err != nil {
		return fmt.Errorf("cannot open the log: %w", err)
	}
	_, werr := fmt.Fprintln(file, line)
	return errors.Join(werr, file.Close())
}

func nextTurnOf(script Script, files Files) (Turn, error) {
	if files.Turns == "" {
		return script.Turn(0), nil
	}
	unlock, err := lockTurns(files.Turns)
	if err != nil {
		return Turn{}, err
	}
	done, countErr := countTurn(files.Turns)
	if err := errors.Join(countErr, unlock()); err != nil {
		return Turn{}, err
	}
	return script.Turn(done), nil
}

const (
	lockRetry    = 5 * time.Millisecond
	lockPatience = 10 * time.Second
)

type turnsLockedError struct {
	Lock string
}

func (e *turnsLockedError) Error() string {
	return "the turn count stayed locked by " + e.Lock + ", remove it if no fake is running"
}

func lockTurns(path string) (func() error, error) {
	return lockTurnsWithin(path, lockPatience)
}

func lockTurnsWithin(path string, patience time.Duration) (func() error, error) {
	lock := filepath.Clean(path + ".lock")
	deadline := time.Now().Add(patience)
	for {
		file, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, privateFile)
		if err == nil {
			return func() error { return errors.Join(file.Close(), os.Remove(lock)) }, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("cannot lock the turn count: %w", err)
		}
		if time.Now().After(deadline) {
			return nil, &turnsLockedError{Lock: lock}
		}
		time.Sleep(lockRetry)
	}
}

func countTurn(path string) (int, error) {
	done := 0
	data, err := os.ReadFile(filepath.Clean(path))
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return 0, fmt.Errorf("cannot read the turn count: %w", err)
	default:
		if done, err = strconv.Atoi(strings.TrimSpace(string(data))); err != nil {
			return 0, fmt.Errorf("cannot parse the turn count: %w", err)
		}
	}
	if err := os.WriteFile(path, []byte(strconv.Itoa(done+1)), privateFile); err != nil {
		return 0, fmt.Errorf("cannot write the turn count: %w", err)
	}
	return done, nil
}
