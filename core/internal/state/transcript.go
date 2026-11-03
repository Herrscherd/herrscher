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
	Role string  `json:"role"`
	Text string  `json:"text"`
	Cost float64 `json:"cost,omitempty"`
	Kind string  `json:"kind,omitempty"`
	// Per-turn usage, present on assistant entries. TokensOut is the turn's
	// output tokens; TokensIn the input; CacheRead/CacheCreate the prompt-cache
	// read/creation tokens; DurMs the turn's wall-clock duration in ms. All
	// omitempty so user turns and pre-usage transcripts stay compact.
	TokensIn    int `json:"tokens_in,omitempty"`
	TokensOut   int `json:"tokens_out,omitempty"`
	CacheRead   int `json:"cache_read,omitempty"`
	CacheCreate int `json:"cache_create,omitempty"`
	DurMs       int `json:"dur_ms,omitempty"`
	CtxTokens   int `json:"ctx_tokens,omitempty"`
}

// TranscriptPath returns the transcript path for session name under dir
// (dir/transcripts/<name>.jsonl), beside participants/<name>.log.
func TranscriptPath(dir, name string) string {
	return filepath.Join(dir, "transcripts", name+".jsonl")
}

const (
	transcriptMaxBytes  = 4 << 20
	transcriptKeepBytes = transcriptMaxBytes / 2
)

// AppendTranscript appends one JSON-line entry. Best-effort: O_APPEND so the
// daemon's single writer never races a read; a missing parent is created. When
// the file crosses transcriptMaxBytes it is compacted to its newest entries, so
// scrollback stays cheap and the transcript is bounded.
func AppendTranscript(path string, entry TranscriptEntry) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), privateDirMode); err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Clean(path), os.O_CREATE|os.O_WRONLY|os.O_APPEND, privateFileMode)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(entry)
	if err != nil {
		closeLogged(file)
		return err
	}
	if _, err := file.Write(append(payload, '\n')); err != nil {
		closeLogged(file)
		return err
	}
	info, statErr := file.Stat()
	if err := file.Close(); err != nil {
		return err
	}
	if statErr == nil && info.Size() > transcriptMaxBytes {
		compactTranscript(path)
	}
	return nil
}

func compactTranscript(path string) {
	all := ReadTranscript(path, 0)
	if len(all) == 0 {
		return
	}
	start := len(all) - 1
	total := 0
	for idx := len(all) - 1; idx >= 0; idx-- {
		payload, err := json.Marshal(all[idx])
		if err != nil {
			continue
		}
		size := len(payload) + 1
		if idx < len(all)-1 && total+size > transcriptKeepBytes {
			break
		}
		total += size
		start = idx
	}
	tmp := path + ".tmp"
	file, err := os.OpenFile(filepath.Clean(tmp), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, privateFileMode)
	if err != nil {
		return
	}
	writer := bufio.NewWriter(file)
	for _, e := range all[start:] {
		payload, err := json.Marshal(e)
		if err != nil {
			continue
		}
		if _, err := writer.Write(append(payload, '\n')); err != nil {
			closeLogged(file)
			removeLogged(tmp)
			return
		}
	}
	if err := writer.Flush(); err != nil {
		closeLogged(file)
		removeLogged(tmp)
		return
	}
	if err := file.Close(); err != nil {
		removeLogged(tmp)
		return
	}
	logFailure("rename", os.Rename(tmp, path))
}

// ReadTranscript returns entries in file order; when limit > 0, only the last
// limit. A missing file yields nil (best-effort observability, never an error).
// A bufio.Reader (not a Scanner) reads lines, so an oversized entry is parsed in
// full rather than silently dropped at a fixed token cap.
func ReadTranscript(path string, limit int) []TranscriptEntry {
	if path == "" {
		return nil
	}
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil
	}
	defer closeLogged(file)
	var out []TranscriptEntry
	reader := bufio.NewReader(file)
	for {
		line, readErr := reader.ReadBytes('\n')
		if trimmed := bytes.TrimSpace(line); len(trimmed) > 0 {
			var entry TranscriptEntry
			if json.Unmarshal(trimmed, &entry) == nil {
				out = append(out, entry)
			}
		}
		if readErr != nil {
			break
		}
	}
	if limit > 0 && len(out) > limit {
		out = append([]TranscriptEntry(nil), out[len(out)-limit:]...)
	}
	return out
}

// TranscriptSummary is what a session list needs from a transcript without
// reading it: when it last moved, and how full the model's window was on the
// last turn that reported usage.
type TranscriptSummary struct {
	LastTs        string
	ContextTokens int
}

const transcriptTailBytes = 64 * 1024

// ReadTranscriptSummary reads only the file's tail. When the newest entry is
// larger than the tail window it falls back to a bounded full read (rare), so a
// timestamp is never lost. Kept separate from ReadTranscript, whose callers need
// the entries themselves.
func ReadTranscriptSummary(path string) TranscriptSummary {
	lines := transcriptTailLines(path)
	var sum TranscriptSummary
	for idx := len(lines) - 1; idx >= 0; idx-- {
		var entry TranscriptEntry
		if json.Unmarshal(lines[idx], &entry) != nil {
			continue
		}
		if sum.LastTs == "" {
			sum.LastTs = entry.Ts
		}
		if sum.ContextTokens == 0 {
			sum.ContextTokens = contextTokens(entry)
		}
		if sum.LastTs != "" && sum.ContextTokens != 0 {
			return sum
		}
	}
	if sum.LastTs != "" {
		return sum
	}
	if all := ReadTranscript(path, 1); len(all) > 0 {
		sum.LastTs = all[len(all)-1].Ts
		sum.ContextTokens = contextTokens(all[len(all)-1])
	}
	return sum
}

// ReadTranscriptLast returns the timestamp of the last recorded entry. Empty
// only when the file is missing or empty.
func ReadTranscriptLast(path string) string {
	return ReadTranscriptSummary(path).LastTs
}

func contextTokens(e TranscriptEntry) int {
	return e.CtxTokens
}

func transcriptTailLines(path string) [][]byte {
	if path == "" {
		return nil
	}
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil
	}
	defer closeLogged(file)
	info, err := file.Stat()
	if err != nil || info.Size() == 0 {
		return nil
	}
	start := info.Size() - transcriptTailBytes
	if start < 0 {
		start = 0
	}
	buf := make([]byte, info.Size()-start)
	if _, err := file.ReadAt(buf, start); err != nil && err != io.EOF {
		return nil
	}
	var out [][]byte
	for _, line := range bytes.Split(buf, []byte{'\n'}) {
		if line = bytes.TrimSpace(line); len(line) > 0 {
			out = append(out, line)
		}
	}
	return out
}

// MoveSessionFile moves a per-session file to the path its session's new name
// gives it. A missing source is not an error: a session renamed before it ever
// spoke has neither transcript nor journal to carry over.
func MoveSessionFile(oldPath, newPath string) error {
	if oldPath == "" || newPath == "" || oldPath == newPath {
		return nil
	}
	if _, err := os.Stat(oldPath); os.IsNotExist(err) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(newPath), privateDirMode); err != nil {
		return err
	}
	return os.Rename(oldPath, newPath)
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
