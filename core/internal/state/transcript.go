package state

import (
	"bufio"
	"encoding/json"
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

// AppendTranscript appends one JSON-line entry. Best-effort: O_APPEND so the
// daemon's single writer never races a read; a missing parent is created.
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
	defer f.Close()
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	return err
}

// ReadTranscript returns entries in file order; when cap > 0, only the last cap.
// A missing file yields nil (best-effort observability, never an error).
func ReadTranscript(path string, cap int) []TranscriptEntry {
	if path == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []TranscriptEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var e TranscriptEntry
		if json.Unmarshal(line, &e) == nil {
			out = append(out, e)
		}
	}
	if cap > 0 && len(out) > cap {
		out = append([]TranscriptEntry(nil), out[len(out)-cap:]...)
	}
	return out
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
