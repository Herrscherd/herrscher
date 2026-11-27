package drives

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/atomicfile"
	"github.com/Herrscherd/herrscher/core/internal/filelock"
)

const (
	journalRotateBytes = 4 << 20
	// JournalGenerations is how many rotated files the journal keeps beside the current one.
	JournalGenerations = 8
	// RefusalsPerWindow is how many refusals of one session the journal records in each RefusalWindow.
	RefusalsPerWindow = 20
	// RefusalWindow is the span over which the refusals of one session are counted.
	RefusalWindow = time.Minute
	lockSuffix    = ".lock"
	refusalsFile  = "refusals.json"
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

// Entry is one line of the journal. Suppressed counts the refusals of the
// same session left out just before it, past RefusalsPerWindow.
type Entry struct {
	Time       time.Time `json:"time"`
	Session    string    `json:"session,omitempty"`
	Agent      string    `json:"agent,omitempty"`
	Drive      string    `json:"drive"`
	Path       string    `json:"path,omitempty"`
	Op         Op        `json:"op"`
	Bytes      int64     `json:"bytes,omitempty"`
	Outcome    Outcome   `json:"outcome"`
	Reason     string    `json:"reason,omitempty"`
	Suppressed int       `json:"suppressed,omitempty"`
}

// Filter narrows a journal read. An empty field matches every entry.
type Filter struct {
	Drive   string
	Agent   string
	Session string
}

func (f Filter) matches(entry *Entry) bool {
	return (f.Drive == "" || f.Drive == entry.Drive) &&
		(f.Agent == "" || f.Agent == entry.Agent) &&
		(f.Session == "" || f.Session == entry.Session)
}

// Log is what a journal read found: the entries, and the lines it could not read.
type Log struct {
	Entries    []Entry
	Unreadable int
}

// Journal is the append only record of what sessions read and wrote on the
// drives. It keeps its current file and JournalGenerations rotated ones, and
// records at most RefusalsPerWindow refusals of one session in each
// RefusalWindow, so a session that keeps being refused cannot push the
// history out.
type Journal struct {
	path string
}

type refusalCount struct {
	Start      time.Time `json:"start"`
	Count      int       `json:"count"`
	Suppressed int       `json:"suppressed"`
}

// Append records one entry.
func (j Journal) Append(ctx context.Context, entry Entry) error {
	if mkdirErr := os.MkdirAll(filepath.Dir(j.path), dirMode); mkdirErr != nil {
		return mkdirErr
	}
	release, err := filelock.Acquire(ctx, j.path+lockSuffix)
	if err != nil {
		return err
	}
	defer release()
	keep, err := j.admitRefusal(&entry)
	if err != nil || !keep {
		return err
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return err
	}
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

func (j Journal) admitRefusal(entry *Entry) (bool, error) {
	if entry.Outcome != OutcomeRefused || entry.Session == "" {
		return true, nil
	}
	counts, err := j.loadRefusals()
	if err != nil {
		return false, err
	}
	count := counts[entry.Session]
	if entry.Time.Sub(count.Start) >= RefusalWindow || entry.Time.Before(count.Start) {
		count.Start, count.Count = entry.Time, 0
	}
	keep := count.Count < RefusalsPerWindow
	if keep {
		count.Count++
		entry.Suppressed, count.Suppressed = count.Suppressed, 0
	} else {
		count.Suppressed++
	}
	counts[entry.Session] = count
	return keep, j.saveRefusals(counts)
}

func (j Journal) refusalsPath() string {
	return filepath.Join(filepath.Dir(j.path), refusalsFile)
}

func (j Journal) loadRefusals() (map[string]refusalCount, error) {
	counts := make(map[string]refusalCount)
	raw, err := os.ReadFile(filepath.Clean(j.refusalsPath()))
	if errors.Is(err, fs.ErrNotExist) {
		return counts, nil
	}
	if err != nil {
		return nil, err
	}
	if decodeErr := json.Unmarshal(raw, &counts); decodeErr != nil {
		slog.Warn("drive refusal counts unreadable, counting again", "err", decodeErr)
		return make(map[string]refusalCount), nil
	}
	return counts, nil
}

func (j Journal) saveRefusals(counts map[string]refusalCount) error {
	raw, err := json.Marshal(counts)
	if err != nil {
		return err
	}
	return atomicfile.Write(j.refusalsPath(), raw, fileMode)
}

func (j Journal) generation(index int) string {
	if index == 0 {
		return j.path
	}
	return j.path + "." + strconv.Itoa(index)
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
	for index := JournalGenerations; index > 0; index-- {
		if err := os.Rename(j.generation(index-1), j.generation(index)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

// Read returns the last entries that match filter, oldest first, at most limit of them.
func (j Journal) Read(filter Filter, limit int) (Log, error) {
	var log Log
	for index := JournalGenerations; index >= 0; index-- {
		if err := readEntries(j.generation(index), filter, &log); err != nil {
			return Log{}, err
		}
	}
	if limit > 0 && len(log.Entries) > limit {
		log.Entries = log.Entries[len(log.Entries)-limit:]
	}
	return log, nil
}

func readEntries(path string, filter Filter, log *Log) error {
	raw, err := os.ReadFile(filepath.Clean(path))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for line := range bytes.Lines(raw) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var entry Entry
		if json.Unmarshal(line, &entry) != nil {
			log.Unreadable++
			continue
		}
		if filter.matches(&entry) {
			log.Entries = append(log.Entries, entry)
		}
	}
	return nil
}
