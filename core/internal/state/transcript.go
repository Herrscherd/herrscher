package state

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
)

// TranscriptEntry is one recorded turn side. Kept separate from the learning
// call-journal: this is the human-visible conversation, replayed as scrollback.
type TranscriptEntry struct {
	Ts   string  `json:"ts"`
	Role string  `json:"role"` // "user" | "assistant"
	Text string  `json:"text"`
	Cost float64 `json:"cost,omitempty"`
	Kind string  `json:"kind,omitempty"` // reserved (tool calls)
}

// TranscriptPath returns the transcript path for session name under dir
// (dir/transcripts/<name>.jsonl), beside participants/<name>.log.
func TranscriptPath(dir, name string) string {
	return filepath.Join(dir, "transcripts", name+".jsonl")
}

const (
	// transcriptMaxBytes triggers compaction once a transcript grows past it, so a
	// long-lived session cannot let its .jsonl grow without bound. transcriptKeepBytes
	// is the budget of newest entries retained on compaction — half the trigger, so
	// ~2 MB of fresh appends must accumulate before the next compaction, never a
	// rewrite on every append.
	transcriptMaxBytes  = 4 << 20
	transcriptKeepBytes = transcriptMaxBytes / 2
)

// AppendTranscript appends one JSON-line entry. Best-effort: O_APPEND so the
// daemon's single writer never races a read; a missing parent is created. When
// the file crosses transcriptMaxBytes it is compacted to its newest entries, so
// scrollback stays cheap and the transcript is bounded.
func AppendTranscript(path string, e TranscriptEntry) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	b, err := json.Marshal(e)
	if err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	fi, statErr := f.Stat()
	f.Close()
	if statErr == nil && fi.Size() > transcriptMaxBytes {
		compactTranscript(path)
	}
	return nil
}

// compactTranscript rewrites path to its newest entries within transcriptKeepBytes,
// via a temp file + atomic rename so a concurrent reader sees the whole old file or
// the whole new one, never a partial write. The single newest entry is always kept,
// even if it alone exceeds the budget. Best-effort: any error leaves the original.
func compactTranscript(path string) {
	all := ReadTranscript(path, 0)
	if len(all) == 0 {
		return
	}
	start := len(all) - 1 // always retain the newest entry
	total := 0
	for i := len(all) - 1; i >= 0; i-- {
		b, err := json.Marshal(all[i])
		if err != nil {
			continue
		}
		sz := len(b) + 1
		if i < len(all)-1 && total+sz > transcriptKeepBytes {
			break
		}
		total += sz
		start = i
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return
	}
	w := bufio.NewWriter(f)
	for _, e := range all[start:] {
		b, err := json.Marshal(e)
		if err != nil {
			continue
		}
		_, _ = w.Write(append(b, '\n'))
	}
	if err := w.Flush(); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return
	}
	_ = os.Rename(tmp, path)
}

// ReadTranscript returns entries in file order; when limit > 0, only the last
// limit. A missing file yields nil (best-effort observability, never an error).
// A bufio.Reader (not a Scanner) reads lines, so an oversized entry is parsed in
// full rather than silently dropped at a fixed token cap.
func ReadTranscript(path string, limit int) []TranscriptEntry {
	if path == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []TranscriptEntry
	r := bufio.NewReader(f)
	for {
		line, readErr := r.ReadBytes('\n')
		if trimmed := bytes.TrimSpace(line); len(trimmed) > 0 {
			var e TranscriptEntry
			if json.Unmarshal(trimmed, &e) == nil {
				out = append(out, e)
			}
		}
		if readErr != nil {
			break // io.EOF (last line) or a read error: stop
		}
	}
	if limit > 0 && len(out) > limit {
		out = append([]TranscriptEntry(nil), out[len(out)-limit:]...)
	}
	return out
}

// ReadTranscriptLast returns the timestamp of the last recorded entry, reading
// only the file's tail so a hot caller (the session list, polled ~1/s) never
// scans a long transcript. Empty only when the file is missing/empty. When the
// newest entry is larger than the tail window it falls back to a bounded full
// read (rare), so a timestamp is never lost. Kept separate from ReadTranscript,
// whose callers need the entries themselves.
func ReadTranscriptLast(path string) string {
	if path == "" {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || fi.Size() == 0 {
		return ""
	}
	const tail = 64 * 1024
	start := fi.Size() - tail
	if start < 0 {
		start = 0
	}
	buf := make([]byte, fi.Size()-start)
	if _, err := f.ReadAt(buf, start); err != nil && err != io.EOF {
		return ""
	}
	// Scan from the end: the last newline-delimited entry that parses is the
	// newest. A partial first line (the window cut mid-entry) is only reached
	// when a single entry exceeds the window, and is left unparsed.
	lines := bytes.Split(buf, []byte{'\n'})
	for i := len(lines) - 1; i >= 0; i-- {
		line := bytes.TrimSpace(lines[i])
		if len(line) == 0 {
			continue
		}
		var e TranscriptEntry
		if json.Unmarshal(line, &e) == nil {
			return e.Ts
		}
		break
	}
	// The newest entry was larger than the tail window (or the window cut it mid-
	// line): fall back to a bounded full read rather than lose the timestamp.
	if all := ReadTranscript(path, 1); len(all) > 0 {
		return all[len(all)-1].Ts
	}
	return ""
}

// RemoveTranscript deletes the transcript at path. A missing file is not an
// error (called on real session removal to avoid leaking transcripts/*.jsonl).
func RemoveTranscript(path string) error {
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
