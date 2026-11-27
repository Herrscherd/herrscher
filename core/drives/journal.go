package drives

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/filelock"
)

const (
	journalRotateBytes = 4 << 20
	rotatedSuffix      = ".1"
	lockSuffix         = ".lock"
)

// Op is what a journal entry records.
type Op string

const (
	// OpList is a listing of a folder.
	OpList Op = "ls"
	// OpRead is a read of a file.
	OpRead Op = "read"
	// OpWrite is a write of a file.
	OpWrite Op = "write"
)

// Outcome is how a journaled operation ended.
type Outcome string

const (
	// OutcomeDone is an operation that ran.
	OutcomeDone Outcome = "done"
	// OutcomeRefused is an operation the access of the agent or the operator refused.
	OutcomeRefused Outcome = "refused"
	// OutcomeFailed is an operation rclone could not carry out.
	OutcomeFailed Outcome = "failed"
)

// Entry is one line of the journal.
type Entry struct {
	Time    time.Time `json:"time"`
	Session string    `json:"session,omitempty"`
	Agent   string    `json:"agent,omitempty"`
	Drive   string    `json:"drive"`
	Path    string    `json:"path,omitempty"`
	Op      Op        `json:"op"`
	Bytes   int64     `json:"bytes,omitempty"`
	Outcome Outcome   `json:"outcome"`
	Reason  string    `json:"reason,omitempty"`
}

// Filter narrows a journal read. An empty field matches every entry.
type Filter struct {
	Drive   string
	Agent   string
	Session string
}

func (f Filter) matches(entry Entry) bool {
	return (f.Drive == "" || f.Drive == entry.Drive) &&
		(f.Agent == "" || f.Agent == entry.Agent) &&
		(f.Session == "" || f.Session == entry.Session)
}

// Journal is the append only record of what sessions read and wrote on the
// drives. It keeps its current file and the one before the last rotation.
type Journal struct {
	path string
}

// Append records one entry.
func (j Journal) Append(ctx context.Context, entry Entry) error {
	line, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	if mkdirErr := os.MkdirAll(filepath.Dir(j.path), dirMode); mkdirErr != nil {
		return mkdirErr
	}
	release, err := filelock.Acquire(ctx, j.path+lockSuffix)
	if err != nil {
		return err
	}
	defer release()
	if rotateErr := j.rotateWhenFull(); rotateErr != nil {
		return rotateErr
	}
	file, err := os.OpenFile(j.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, fileMode)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(append(line, '\n'))
	return errors.Join(writeErr, file.Close())
}

func (j Journal) rotateWhenFull() error {
	info, err := os.Stat(j.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Size() < journalRotateBytes {
		return nil
	}
	return os.Rename(j.path, j.path+rotatedSuffix)
}

// Read returns the last entries that match filter, oldest first, at most limit of them.
func (j Journal) Read(filter Filter, limit int) ([]Entry, error) {
	var kept []Entry
	for _, path := range []string{j.path + rotatedSuffix, j.path} {
		entries, err := readEntries(path, filter)
		if err != nil {
			return nil, err
		}
		kept = append(kept, entries...)
	}
	if limit > 0 && len(kept) > limit {
		kept = kept[len(kept)-limit:]
	}
	return kept, nil
}

func readEntries(path string, filter Filter) ([]Entry, error) {
	raw, err := os.ReadFile(filepath.Clean(path))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []Entry
	for line := range bytes.Lines(raw) {
		var entry Entry
		if json.Unmarshal(line, &entry) != nil {
			continue
		}
		if filter.matches(entry) {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}
