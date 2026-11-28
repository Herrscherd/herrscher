package drives

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/atomicfile"
	"github.com/Herrscherd/herrscher/core/internal/filelock"
)

const (
	journalRotateBytes = 4 << 20
	// JournalGenerations is how many rotated files each journal file keeps beside the current one.
	JournalGenerations = 8
	// RefusalsPerWindow is how many refusals of one session the journal records in each Window.
	RefusalsPerWindow = 60
	// OperationsPerWindow is how many listings, reads and writes one session runs in each Window; past it, the next is refused before it runs.
	OperationsPerWindow = 600
	// Window is the span over which the refusals and the operations of one session are counted.
	Window         = time.Minute
	lockFile       = "journal.lock"
	ratesFile      = "session-rates.json"
	activitySuffix = ".jsonl"
	writesSuffix   = ".writes.jsonl"
	operatorFile   = "operator"
	sessionPrefix  = "session-"
	sessionKeyHex  = 16
	suppressedKept = 24 * time.Hour
	// JournalRetention is how long the files of a session that journals nothing more are kept; the operator's are kept until the size cap needs their room.
	JournalRetention = 30 * 24 * time.Hour
	// JournalMaxBytes caps the whole journal; past it, the oldest files go first, the operator's writes last.
	JournalMaxBytes = 512 << 20
	// JournalReadFiles is how many files, the most recently written, an unfiltered read looks at.
	JournalReadFiles = 64
	pruneMarker      = "pruned-at"
	pruneEvery       = time.Hour
)

// OperationCapError reports a session that ran OperationsPerWindow operations
// within Window: the next one is refused before it runs, so nothing runs
// unrecorded.
type OperationCapError struct {
	Limit  int
	Window time.Duration
}

func (e *OperationCapError) Error() string {
	return fmt.Sprintf("this session ran %d drive operations within %s: wait before the next one", e.Limit, e.Window)
}

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
	// OutcomeUnsent is a write that failed before anything reached the remote, such as a missing source.
	OutcomeUnsent Outcome = "unsent"
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

func (e *Entry) carriesNoData() bool {
	return e.Outcome == OutcomeRefused || e.Outcome == OutcomeUnsent
}

func (e *Entry) reachedTheRemoteAsWrite() bool {
	return e.Op == OpWrite && (e.Outcome == OutcomeDone || e.Outcome == OutcomeFailed)
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
// drives. Each session, and the operator, has files of its own, so one session
// can only ever rotate its own history away. Every listing, read and write
// that ran is recorded; writes that reached the remote go to a file of their
// own, which nothing else fills. Only the entries that carry no data, the
// refusals and the writes that never left the machine, are capped at
// RefusalsPerWindow per session. Each file keeps JournalGenerations rotated
// ones beside it.
type Journal struct {
	dir string
}

type sessionRate struct {
	Start      time.Time `json:"start"`
	Operations int       `json:"operations"`
	Refusals   int       `json:"refusals"`
	Suppressed int       `json:"suppressed"`
}

func fileKey(session string) string {
	if session == "" {
		return operatorFile
	}
	sum := sha256.Sum256([]byte(session))
	return sessionPrefix + hex.EncodeToString(sum[:])[:sessionKeyHex]
}

// Reserve counts one operation of session and refuses it with an
// OperationCapError past OperationsPerWindow, before anything runs. The
// operator is never capped.
func (j Journal) Reserve(ctx context.Context, session string, now time.Time) error {
	if session == "" {
		return nil
	}
	release, err := j.lock(ctx)
	if err != nil {
		return err
	}
	defer release()
	rates, err := j.loadRates()
	if err != nil {
		return err
	}
	rate := current(rates[session], now)
	if rate.Operations >= OperationsPerWindow {
		return &OperationCapError{Limit: OperationsPerWindow, Window: Window}
	}
	rate.Operations++
	rates[session] = rate
	return j.saveRates(prune(rates, session, now))
}

// Append records one entry. It fails when the entry could not be written, and
// a caller that ran an operation reports that failure instead of its result.
func (j Journal) Append(ctx context.Context, entry Entry) error {
	release, err := j.lock(ctx)
	if err != nil {
		return err
	}
	defer release()
	if entry.carriesNoData() && entry.Session != "" {
		keep, admitErr := j.admitRefusal(&entry)
		if admitErr != nil || !keep {
			return admitErr
		}
	}
	target := filepath.Join(j.dir, fileKey(entry.Session)+activitySuffix)
	if entry.reachedTheRemoteAsWrite() {
		target = filepath.Join(j.dir, fileKey(entry.Session)+writesSuffix)
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	if rotateErr := rotateWhenFull(target); rotateErr != nil {
		return rotateErr
	}
	j.pruneWhenDue(entry.Time)
	file, err := os.OpenFile(filepath.Clean(target), os.O_APPEND|os.O_CREATE|os.O_WRONLY, fileMode)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(append(line, '\n'))
	return errors.Join(writeErr, file.Close())
}

func (j Journal) lock(ctx context.Context) (func(), error) {
	if err := os.MkdirAll(j.dir, dirMode); err != nil {
		return nil, err
	}
	return filelock.Acquire(ctx, filepath.Join(j.dir, lockFile))
}

func (j Journal) admitRefusal(entry *Entry) (bool, error) {
	rates, err := j.loadRates()
	if err != nil {
		return false, err
	}
	rate := current(rates[entry.Session], entry.Time)
	keep := rate.Refusals < RefusalsPerWindow
	if keep {
		rate.Refusals++
		entry.Suppressed, rate.Suppressed = rate.Suppressed, 0
	} else {
		rate.Suppressed++
	}
	rates[entry.Session] = rate
	return keep, j.saveRates(prune(rates, entry.Session, entry.Time))
}

func current(rate sessionRate, now time.Time) sessionRate {
	if windowEnded(rate, now) {
		rate.Start, rate.Operations, rate.Refusals = now, 0, 0
	}
	return rate
}

func prune(rates map[string]sessionRate, keep string, now time.Time) map[string]sessionRate {
	for session, rate := range rates {
		if session != keep && windowEnded(rate, now) && (rate.Suppressed == 0 || now.Sub(rate.Start) > suppressedKept) {
			delete(rates, session)
		}
	}
	return rates
}

func windowEnded(rate sessionRate, now time.Time) bool {
	return now.Sub(rate.Start) >= Window || now.Before(rate.Start)
}

func (j Journal) ratesPath() string {
	return filepath.Join(j.dir, ratesFile)
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
// limit of them, or all of them when limit is 0. A filter on a session reads
// that session's files alone. Each file is read newest first, and its reading
// stops once it holds limit entries.
func (j Journal) Read(filter Filter, limit int) (Log, error) {
	files, err := j.files(filter.Session)
	if err != nil {
		return Log{}, err
	}
	var log Log
	var merged []Entry
	for _, path := range files {
		found, readErr := newestEntries(path, filter, limit, &log)
		if readErr != nil {
			return Log{}, readErr
		}
		merged = append(merged, found...)
	}
	slices.SortStableFunc(merged, func(left, right Entry) int { return right.Time.Compare(left.Time) })
	if limit > 0 && len(merged) > limit {
		merged = merged[:limit]
	}
	slices.Reverse(merged)
	log.Entries = merged
	return log, nil
}

func (j Journal) files(session string) ([]string, error) {
	if session != "" {
		key := fileKey(session)
		return []string{filepath.Join(j.dir, key+activitySuffix), filepath.Join(j.dir, key+writesSuffix)}, nil
	}
	found, err := j.journalFiles()
	if err != nil {
		return nil, err
	}
	var current []journalFile
	for _, file := range found {
		if strings.HasSuffix(file.path, activitySuffix) {
			current = append(current, file)
		}
	}
	slices.SortFunc(current, func(left, right journalFile) int { return right.modified.Compare(left.modified) })
	paths := make([]string, 0, min(len(current), JournalReadFiles))
	for index := range min(len(current), JournalReadFiles) {
		paths = append(paths, current[index].path)
	}
	return paths, nil
}

type journalFile struct {
	path     string
	size     int64
	modified time.Time
}

func (f journalFile) operator() bool {
	return strings.HasPrefix(filepath.Base(f.path), operatorFile+".")
}

func (f journalFile) operatorWrites() bool {
	return strings.HasPrefix(filepath.Base(f.path), operatorFile+writesSuffix)
}

func (j Journal) journalFiles() ([]journalFile, error) {
	entries, err := os.ReadDir(j.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var files []journalFile
	for _, entry := range entries {
		name := entry.Name()
		if !entry.Type().IsRegular() || !strings.Contains(name, ".jsonl") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, journalFile{path: filepath.Join(j.dir, name), size: info.Size(), modified: info.ModTime()})
	}
	return files, nil
}

func (j Journal) pruneWhenDue(now time.Time) {
	marker := filepath.Join(j.dir, pruneMarker)
	if info, err := os.Stat(marker); err == nil && now.Sub(info.ModTime()) < pruneEvery && !now.Before(info.ModTime()) {
		return
	}
	if err := j.pruneFiles(now); err != nil {
		slog.Warn("drive journal prune", "err", err)
	}
	if err := os.WriteFile(marker, nil, fileMode); err != nil {
		slog.Warn("drive journal prune marker", "err", err)
		return
	}
	if err := os.Chtimes(marker, now, now); err != nil {
		slog.Warn("drive journal prune marker", "err", err)
	}
}

// pruneFiles drops the files of sessions that journaled nothing for
// JournalRetention, then, while the journal holds more than JournalMaxBytes,
// the least recently written files, the operator's writes last.
func (j Journal) pruneFiles(now time.Time) error {
	files, err := j.journalFiles()
	if err != nil {
		return err
	}
	var kept []journalFile
	var total int64
	for _, file := range files {
		if !file.operator() && now.Sub(file.modified) > JournalRetention {
			if err := os.Remove(file.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			continue
		}
		kept = append(kept, file)
		total += file.size
	}
	slices.SortFunc(kept, func(left, right journalFile) int {
		if left.operatorWrites() != right.operatorWrites() {
			if left.operatorWrites() {
				return 1
			}
			return -1
		}
		return left.modified.Compare(right.modified)
	})
	for _, file := range kept {
		if total <= JournalMaxBytes {
			break
		}
		if err := os.Remove(file.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		total -= file.size
	}
	return nil
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
