package usage

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Retention is how long a daily ledger file is kept before it is pruned.
const Retention = 120 * day

// ErrLedgerTruncated reports a daily ledger file too large to be read whole, so that the
// totals of its day would be wrong.
var ErrLedgerTruncated = errors.New("usage: a day of the ledger is too large, its totals would be wrong")

const (
	dayLayout     = time.DateOnly
	fileExt       = ".jsonl"
	maxLedgerFile = 64 << 20
	maxLedgerLine = 64 << 10
	dirMode       = 0o700
	fileMode      = 0o600
)

// Record is one line of the ledger: what one model spent during one terminal reply.
type Record struct {
	Time        time.Time `json:"time"`
	Session     string    `json:"session"`
	Vendor      string    `json:"vendor"`
	Account     string    `json:"account"`
	Model       string    `json:"model"`
	In          int64     `json:"in"`
	Out         int64     `json:"out"`
	CacheRead   int64     `json:"cacheRead"`
	CacheCreate int64     `json:"cacheCreate"`
	Cost        float64   `json:"cost"`

	CacheCreateHour int64       `json:"cacheCreateHour,omitempty"`
	Fast            bool        `json:"fast,omitempty"`
	Split           CostSplit   `json:"split,omitzero"`
	Saved           float64     `json:"saved,omitempty"`
	Compaction      *Compaction `json:"compaction,omitempty"`
	Unpriced        bool        `json:"-"`
}

// Ledger is the directory of daily JSON lines files, one file per UTC day.
type Ledger struct {
	Dir string
	Now func() time.Time
}

// Append writes the records to the files of their days, then prunes the files
// past the retention.
func (l Ledger) Append(records []Record) error {
	if len(records) == 0 {
		return nil
	}
	lines := make(map[string][]byte)
	for index := range records {
		line, err := json.Marshal(records[index])
		if err != nil {
			return fmt.Errorf("usage record: %w", err)
		}
		name := fileName(records[index].Time)
		lines[name] = append(append(lines[name], line...), '\n')
	}
	if err := writeDays(l.Dir, lines); err != nil {
		return err
	}
	return l.Prune()
}

func writeDays(dir string, lines map[string][]byte) error {
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("usage ledger: %w", err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return fmt.Errorf("usage ledger: %w", err)
	}
	for _, name := range slices.Sorted(maps.Keys(lines)) {
		if err := appendFile(root, name, lines[name]); err != nil {
			return errors.Join(err, root.Close())
		}
	}
	return root.Close()
}

// Prune removes the daily files older than the retention.
func (l Ledger) Prune() error {
	entries, err := os.ReadDir(l.Dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("usage ledger: %w", err)
	}
	cutoff := truncDay(l.Now().Add(-Retention))
	for _, entry := range entries {
		at, ok := dayOf(entry.Name())
		if !ok || !at.Before(cutoff) {
			continue
		}
		if err := os.Remove(filepath.Join(l.Dir, entry.Name())); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("usage ledger: %w", err)
		}
	}
	return nil
}

// Records reads the records of the period, from the files of its days only, and
// skips the lines that do not decode. A day too large to read is left out and reported
// with ErrLedgerTruncated, the readable days are still returned.
func (l Ledger) Records(rng Range) ([]Record, error) {
	now := l.Now().UTC()
	since := now.Add(-rng.Span())
	root, err := os.OpenRoot(l.Dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("usage ledger: %w", err)
	}
	out, readErr := readDays(root, since, now)
	return out, errors.Join(readErr, root.Close())
}

func readDays(root *os.Root, since, now time.Time) ([]Record, error) {
	var out []Record
	var truncated error
	for at := truncDay(since); !at.After(now); at = at.AddDate(0, 0, 1) {
		name := fileName(at)
		records, err := readDay(root, name, since)
		if errors.Is(err, ErrLedgerTruncated) {
			slog.Warn("usage ledger: day left out, too large to read", "file", name)
			truncated = err
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, records...)
	}
	return out, truncated
}

func readDay(root *os.Root, name string, since time.Time) ([]Record, error) {
	file, err := root.Open(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("usage ledger: %w", err)
	}
	records, readErr := readLines(file, name, since)
	return records, errors.Join(readErr, file.Close())
}

func readLines(file *os.File, name string, since time.Time) ([]Record, error) {
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("usage ledger %s: %w", name, err)
	}
	if info.Size() > maxLedgerFile {
		return nil, ErrLedgerTruncated
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(nil, maxLedgerLine)
	var out []Record
	for scanner.Scan() {
		var record Record
		if json.Unmarshal(scanner.Bytes(), &record) != nil {
			continue
		}
		if record.Time.Before(since) {
			continue
		}
		out = append(out, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("usage ledger %s: %w", name, err)
	}
	return out, nil
}

func appendFile(root *os.Root, name string, data []byte) error {
	file, err := root.OpenFile(name, os.O_APPEND|os.O_CREATE|os.O_WRONLY, fileMode)
	if err != nil {
		return fmt.Errorf("usage ledger: %w", err)
	}
	_, writeErr := file.Write(data)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return fmt.Errorf("usage ledger: %w", err)
	}
	return nil
}

func fileName(at time.Time) string {
	return at.UTC().Format(dayLayout) + fileExt
}

func dayOf(name string) (time.Time, bool) {
	base, ok := strings.CutSuffix(name, fileExt)
	if !ok {
		return time.Time{}, false
	}
	at, err := time.Parse(dayLayout, base)
	return at, err == nil
}

func truncDay(instant time.Time) time.Time {
	utc := instant.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
}
