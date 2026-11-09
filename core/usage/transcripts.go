package usage

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const (
	claudeTranscripts = "projects"
	codexTranscripts  = "sessions"
	indexVersion      = 1
	seenKeyBytes      = 8
	indexSaveEvery    = 30 * time.Second
)

// TranscriptSource is the home of a vendor CLI whose session transcripts are read:
// CLAUDE_CONFIG_DIR for Claude, CODEX_HOME for Codex.
type TranscriptSource struct {
	Vendor  string
	Account string
	Home    string
}

func (s TranscriptSource) dir() (string, bool) {
	switch s.Vendor {
	case contracts.VendorClaude:
		return filepath.Join(s.Home, claudeTranscripts), true
	case contracts.VendorCodex:
		return filepath.Join(s.Home, codexTranscripts), true
	}
	return "", false
}

// TranscriptIndex totals the tokens of the vendor transcripts by hour, session and model,
// and keeps the totals in a file of the state directory, so a transcript is read once
// and only its new lines afterwards. Scan runs from one goroutine at a time; Records,
// Covered and Scanned answer from the totals last published, never waiting on a scan.
type TranscriptIndex struct {
	Path string
	Now  func() time.Time

	scanMu  sync.Mutex
	loaded  bool
	files   map[string]*transcriptFile
	seen    map[uint64]struct{}
	savedAt time.Time

	mu      sync.Mutex
	view    []Record
	covered map[AccountKey]bool
	scanned bool
}

type transcriptFile struct {
	Vendor  string    `json:"vendor"`
	Account string    `json:"account,omitempty"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
	Offset  int64     `json:"offset"`
	Records []Record  `json:"records,omitempty"`
}

type indexFile struct {
	Version int                        `json:"version"`
	Files   map[string]*transcriptFile `json:"files"`
	Seen    []byte                     `json:"seen"`
}

// Scanned tells whether a scan of every source has finished since the daemon started.
func (x *TranscriptIndex) Scanned() bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.scanned
}

// Covered is the accounts whose transcripts are read, the ledger records of which the
// transcripts replace.
func (x *TranscriptIndex) Covered() map[AccountKey]bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	return maps.Clone(x.covered)
}

// Records is the totals of the hours that end after since.
func (x *TranscriptIndex) Records(since time.Time) []Record {
	from := since.UTC().Truncate(time.Hour)
	x.mu.Lock()
	defer x.mu.Unlock()
	var out []Record
	for index := range x.view {
		if !x.view[index].Time.Before(from) {
			out = append(out, x.view[index])
		}
	}
	return out
}

// Scan reads the new lines of every transcript of the sources, then saves the index. A
// transcript two sources share through a link is read for the first source only. The
// totals are published after each source, and the index saved at most every
// indexSaveEvery while a long scan runs, so a stopped scan resumes where it was.
func (x *TranscriptIndex) Scan(ctx context.Context, sources []TranscriptSource) error {
	x.scanMu.Lock()
	defer x.scanMu.Unlock()
	x.load()
	covered := make(map[AccountKey]bool)
	dirs := make(map[string]bool)
	cutoff := x.Now().Add(-Retention)
	var scanErr error
	for _, source := range sources {
		dir, ok := source.dir()
		if !ok {
			continue
		}
		resolved, err := filepath.EvalSymlinks(dir)
		if err != nil || dirs[resolved] {
			continue
		}
		dirs[resolved] = true
		covered[AccountKey{Vendor: source.Vendor, Account: source.Account}] = true
		scanErr = x.scanDir(ctx, source, resolved, cutoff)
		x.publish(covered, false)
		if scanErr != nil {
			break
		}
	}
	x.prune(cutoff)
	x.publish(covered, scanErr == nil)
	return errors.Join(scanErr, x.save())
}

func (x *TranscriptIndex) publish(covered map[AccountKey]bool, done bool) {
	var view []Record
	for _, file := range x.files {
		view = append(view, file.Records...)
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	x.view, x.covered = view, maps.Clone(covered)
	x.scanned = x.scanned || done
}

func (x *TranscriptIndex) scanDir(ctx context.Context, source TranscriptSource, dir string, cutoff time.Time) error {
	root, ok := openTranscripts(dir)
	if !ok {
		return nil
	}
	walkErr := fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		info, ok := recentTranscript(name, entry, walkErr, cutoff)
		if !ok {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := x.scanFile(ctx, source, root, name, info); err != nil {
			return err
		}
		return x.saveEvery()
	})
	return errors.Join(walkErr, root.Close())
}

func openTranscripts(dir string) (*os.Root, bool) {
	root, err := os.OpenRoot(dir)
	return root, err == nil
}

func recentTranscript(name string, entry fs.DirEntry, walkErr error, cutoff time.Time) (fs.FileInfo, bool) {
	if walkErr != nil || entry.IsDir() || !strings.HasSuffix(name, fileExt) {
		return nil, false
	}
	info, err := entry.Info()
	return info, err == nil && !info.ModTime().Before(cutoff)
}

func (x *TranscriptIndex) scanFile(ctx context.Context, source TranscriptSource, root *os.Root, name string, info fs.FileInfo) error {
	path := filepath.Join(root.Name(), name)
	known, ok := x.files[path]
	if ok && known.Size == info.Size() && known.ModTime.Equal(info.ModTime()) {
		return nil
	}
	appended := ok && source.Vendor == contracts.VendorClaude
	if appended && info.Size() < known.Offset {
		known.Size, known.ModTime, known.Offset = info.Size(), info.ModTime(), info.Size()
		return nil
	}
	file := &transcriptFile{Vendor: source.Vendor, Account: source.Account}
	if appended {
		file = known
	}
	consumed, err := x.readFile(ctx, root, name, file)
	file.Offset += consumed
	file.Size, file.ModTime = info.Size(), info.ModTime()
	if err != nil {
		file.Size = -1
	}
	x.files[path] = file
	return err
}

func (x *TranscriptIndex) readFile(ctx context.Context, root *os.Root, name string, file *transcriptFile) (int64, error) {
	handle, err := root.Open(name)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("usage transcript: %w", err)
	}
	if _, err := handle.Seek(file.Offset, io.SeekStart); err != nil {
		return 0, errors.Join(fmt.Errorf("usage transcript: %w", err), handle.Close())
	}
	totals := newTally(file.Vendor, file.Account, file.Records)
	read := x.lineReader(file.Vendor)
	consumed, scanErr := scanTranscript(ctx, handle, func(line []byte) { read(line, totals.add) })
	file.Records = totals.records
	return consumed, errors.Join(scanErr, handle.Close())
}

func (x *TranscriptIndex) lineReader(vendor string) func([]byte, func(tokenEvent)) {
	if vendor == contracts.VendorCodex {
		return (&codexReader{}).read
	}
	return claudeReader{seen: x.seen}.read
}

func (x *TranscriptIndex) prune(cutoff time.Time) {
	for path, file := range x.files {
		if file.ModTime.Before(cutoff) {
			delete(x.files, path)
		}
	}
}

func (x *TranscriptIndex) load() {
	if x.loaded {
		return
	}
	x.loaded = true
	x.files = make(map[string]*transcriptFile)
	x.seen = make(map[uint64]struct{})
	raw, err := os.ReadFile(x.Path)
	if err != nil {
		return
	}
	var saved indexFile
	if json.Unmarshal(raw, &saved) != nil || saved.Version != indexVersion || saved.Files == nil {
		return
	}
	x.files = saved.Files
	for at := 0; at+seenKeyBytes <= len(saved.Seen); at += seenKeyBytes {
		x.seen[binary.LittleEndian.Uint64(saved.Seen[at:])] = struct{}{}
	}
}

func (x *TranscriptIndex) saveEvery() error {
	if x.Now().Sub(x.savedAt) < indexSaveEvery {
		return nil
	}
	return x.save()
}

func (x *TranscriptIndex) save() error {
	x.savedAt = x.Now()
	seen := make([]byte, 0, len(x.seen)*seenKeyBytes)
	for key := range x.seen {
		seen = binary.LittleEndian.AppendUint64(seen, key)
	}
	raw, err := json.Marshal(indexFile{Version: indexVersion, Files: x.files, Seen: seen})
	if err != nil {
		return fmt.Errorf("usage transcript index: %w", err)
	}
	return writeAtomic(x.Path, raw)
}
