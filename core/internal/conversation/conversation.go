package conversation

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const Start = "start"

var ErrUnsupported = errors.New("conversation restore is not supported by this backend")

type record struct {
	UUID string `json:"uuid"`
}

func Position(home, dir, token string) (string, error) {
	if token == "" {
		return Start, nil
	}
	lines, err := readLines(TranscriptPath(home, dir, token))
	if err != nil {
		return "", err
	}
	last := ""
	for _, line := range lines {
		if id := uuidOf(line); id != "" {
			last = id
		}
	}
	if last == "" {
		return Start, nil
	}
	return last, nil
}

func Fork(home, srcDir, token, position, dstDir, newToken string) (string, error) {
	if position == Start {
		return "", nil
	}
	lines, err := readLines(TranscriptPath(home, srcDir, token))
	if err != nil {
		return "", err
	}
	cut := -1
	for i, line := range lines {
		if uuidOf(line) == position {
			cut = i
			break
		}
	}
	if cut < 0 {
		return "", fmt.Errorf("conversation: message %s is not in the transcript of %s", position, token)
	}
	var out bytes.Buffer
	for _, line := range lines[:cut+1] {
		rewritten, err := withSessionID(line, newToken)
		if err != nil {
			return "", err
		}
		out.Write(rewritten)
		out.WriteByte('\n')
	}
	target := TranscriptPath(home, dstDir, newToken)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(target, out.Bytes(), 0o600); err != nil {
		return "", err
	}
	return newToken, nil
}

func NewToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

func TranscriptPath(home, dir, token string) string {
	return filepath.Join(home, ".claude", "projects", encodeDir(dir), token+".jsonl")
}

func encodeDir(dir string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '-'
	}, dir)
}

func readLines(path string) ([][]byte, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w: no transcript at %s", ErrUnsupported, path)
	}
	if err != nil {
		return nil, err
	}
	var lines [][]byte
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 0, 1<<20), 64<<20)
	for scanner.Scan() {
		if line := bytes.TrimSpace(scanner.Bytes()); len(line) > 0 {
			lines = append(lines, append([]byte(nil), line...))
		}
	}
	return lines, scanner.Err()
}

func uuidOf(line []byte) string {
	var r record
	if json.Unmarshal(line, &r) != nil {
		return ""
	}
	return r.UUID
}

func withSessionID(line []byte, sessionID string) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(line, &fields); err != nil {
		return line, nil
	}
	if _, ok := fields["sessionId"]; !ok {
		return line, nil
	}
	id, err := json.Marshal(sessionID)
	if err != nil {
		return nil, err
	}
	fields["sessionId"] = id
	return json.Marshal(fields)
}
