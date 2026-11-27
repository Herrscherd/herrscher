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
	"slices"
	"strconv"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/atomicfile"
	"github.com/Herrscherd/herrscher/core/internal/filelock"
)

const (
	journalRotateBytes = 4 << 20
	// JournalGenerations is how many rotated files each journal file keeps beside the current one.
	JournalGenerations = 8
	// EntriesPerWindow is how many listings, reads and refusals of one session the journal records in each EntryWindow.
	EntriesPerWindow = 60
	// EntryWindow is the span over which the entries of one session are counted.
	EntryWindow    = time.Minute
	lockSuffix     = ".lock"
	ratesFile      = "session-rates.json"
	suppressedKept = 24 * time.Hour
	writesFile     = "writes.jsonl"
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

// Entry is one line of the journal. Suppressed counts the entries of the
// same session left out just before it, past EntriesPerWindow.
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
// drives. Writes go to a file of their own, so no amount of listing or
// reading pushes a write out of the history. The other file records at most
// EntriesPerWindow entries of one session in each EntryWindow. Each file keeps
// JournalGenerations rotated ones beside it.
type Journal struct {
	path string
}

type sessionRate struct {
	Start      time.Time `json:"start"`
	Count      int       `json:"count"`
	Suppressed int       `json:"suppressed"`
}

func isWrite(entry *Entry) bool {
	return entry.Op == OpWrite && entry.Outcome != OutcomeRefused
}

func (j Journal) writesPath() string {
	return filepath.Join(filepath.Dir(j.path), writesFile)
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
	target := j.writesPath()
	if !isWrite(&entry) {
		target = j.path
		keep, admitErr := j.admit(&entry)
		if admitErr != nil || !keep {
			return admitErr
		}
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	if rotateErr := rotateWhenFull(target); rotateErr != nil {
		return rotateErr
	}
	file, err := os.OpenFile(filepath.Clean(target), os.O_APPEND|os.O_CREATE|os.O_WRONLY, fileMode)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(append(line, '\n'))
	return errors.Join(writeErr, file.Close())
}

func (j Journal) admit(entry *Entry) (bool, error) {
	if entry.Session == "" {
		return true, nil
	}
	rates, err := j.loadRates()
	if err != nil {
		return false, err
	}
	rate := rates[entry.Session]
	if windowEnded(rate, entry.Time) {
		rate.Start, rate.Count = entry.Time, 0
	}
	keep := rate.Count < EntriesPerWindow
	if keep {
		rate.Count++
		entry.Suppressed, rate.Suppressed = rate.Suppressed, 0
	} else {
		rate.Suppressed++
	}
	rates[entry.Session] = rate
	for session, other := range rates {
		if session != entry.Session && windowEnded(other, entry.Time) && (other.Suppressed == 0 || entry.Time.Sub(other.Start) > suppressedKept) {
			delete(rates, session)
		}
	}
	return keep, j.saveRates(rates)
}

func windowEnded(rate sessionRate, now time.Time) bool {
	return now.Sub(rate.Start) >= EntryWindow || now.Before(rate.Start)
}

func (j Journal) ratesPath() string {
	return filepath.Join(filepath.Dir(j.path), ratesFile)
}

func (j Journal) loadRates() (map[string]sessionRate, error) {
	rates := make(map[string]sessionRate)
	raw, err := os.ReadFile(filepath.Clean(j.ratesPath()))
	if errors.Is(err, fs.ErrNotExist) {
		return rates, nil
	}
	if err != nil {
		return nil, err
	}
	if decodeErr := json.Unmarshal(raw, &rates); decodeErr != nil {
		slog.Warn("drive session rates unreadable, counting again", "err", decodeErr)
		return make(map[string]sessionRate), nil
	}
	return rates, nil
}

func (j Journal) saveRates(rates map[string]sessionRate) error {
	raw, err := json.Marshal(rates)
	if err != nil {
		return err
	}
	return atomicfile.Write(j.ratesPath(), raw, fileMode)
}

func generation(path string, index int) string {
	if index == 0 {
		return path
	}
	return path + "." + strconv.Itoa(index)
}

func rotateWhenFull(path string) error {
	info, err := os.Stat(path)
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
		if err := os.Rename(generation(path, index-1), generation(path, index)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

// Read returns the last entries that match filter, oldest first, at most
// limit of them, or all of them when limit is 0. It reads newest first and
// stops once it holds limit entries of each file.
func (j Journal) Read(filter Filter, limit int) (Log, error) {
	var log Log
	activity, err := newestEntries(j.path, filter, limit, &log)
	if err != nil {
		return Log{}, err
	}
	writes, err := newestEntries(j.writesPath(), filter, limit, &log)
	if err != nil {
		return Log{}, err
	}
	merged := slices.Concat(activity, writes)
	slices.SortStableFunc(merged, func(left, right Entry) int { return right.Time.Compare(left.Time) })
	if limit > 0 && len(merged) > limit {
		merged = merged[:limit]
	}
	slices.Reverse(merged)
	log.Entries = merged
	return log, nil
}

func newestEntries(path string, filter Filter, limit int, log *Log) ([]Entry, error) {
	var found []Entry
	for index := 0; index <= JournalGenerations; index++ {
		raw, err := os.ReadFile(filepath.Clean(generation(path, index)))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		lines := bytes.Split(bytes.TrimRight(raw, "\n"), []byte("\n"))
		for line := len(lines) - 1; line >= 0; line-- {
			if limit > 0 && len(found) == limit {
				return found, nil
			}
			found = appendMatch(found, lines[line], filter, log)
		}
	}
	return found, nil
}

func appendMatch(found []Entry, line []byte, filter Filter, log *Log) []Entry {
	if len(bytes.TrimSpace(line)) == 0 {
		return found
	}
	var entry Entry
	if json.Unmarshal(line, &entry) != nil {
		log.Unreadable++
		return found
	}
	if filter.matches(&entry) {
		found = append(found, entry)
	}
	return found
}
