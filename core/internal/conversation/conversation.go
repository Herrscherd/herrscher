// Package conversation reads and forks the transcripts that a Claude backend
// keeps for each conversation, so a session can return to an earlier message
// and continue from there under a new conversation id. Only the Claude
// transcript layout is known; any other backend is reported as unsupported.
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

const (
	// Start is the position of a conversation before its first message.
	Start = "start"

	projectsDirName      = "projects"
	claudeDirName        = ".claude"
	transcriptExtension  = ".jsonl"
	dirMode              = 0o750
	fileMode             = 0o600
	scannerInitialBuffer = 1 << 20
	scannerMaxLine       = 64 << 20
	uuidBytes            = 16
	uuidVersionIndex     = 6
	uuidVariantIndex     = 8
	uuidVersionBits      = 0x0f
	uuidVersion4         = 0x40
	uuidVariantBits      = 0x3f
	uuidVariant10        = 0x80
	sessionIDKey         = "sessionId"
)

var (
	// ErrUnsupported is returned when a backend keeps no transcript this package can read.
	ErrUnsupported = errors.New("conversation restore is not supported by this backend")
	// ErrMessageNotFound is returned when a transcript has no message with the asked id.
	ErrMessageNotFound = errors.New("message is not in the transcript")
)

type record struct {
	UUID string `json:"uuid"`
}

// Position returns the id of the last message of the transcript that token
// names in dir, or [Start] when there is no token or no message yet.
func Position(home, dir, token string) (string, error) {
	if token == "" {
		return Start, nil
	}
	lines, err := readLines(home, dir, token)
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

// Fork copies the transcript of token in srcDir up to and including message
// position, rewrites its session id to newToken, and stores it for dstDir. It
// returns newToken, or an empty token when position is [Start], which has no
// transcript to copy. It returns an error wrapping [ErrMessageNotFound] when
// position is not in the transcript.
func Fork(home, srcDir, token, position, dstDir, newToken string) (string, error) {
	if position == Start {
		return "", nil
	}
	lines, err := readLines(home, srcDir, token)
	if err != nil {
		return "", err
	}
	cut := lineOf(lines, position)
	if cut < 0 {
		return "", fmt.Errorf("%w: %s in %s", ErrMessageNotFound, position, token)
	}
	var out bytes.Buffer
	for _, line := range lines[:cut+1] {
		rewritten, rewriteErr := withSessionID(line, newToken)
		if rewriteErr != nil {
			return "", rewriteErr
		}
		out.Write(rewritten)
		out.WriteByte('\n')
	}
	target := TranscriptPath(home, dstDir, newToken)
	if err := os.MkdirAll(filepath.Dir(target), dirMode); err != nil {
		return "", err
	}
	if err := os.WriteFile(target, out.Bytes(), fileMode); err != nil {
		return "", err
	}
	return newToken, nil
}

// NewToken returns a random version 4 UUID, the id a forked conversation takes.
func NewToken() (string, error) {
	var randomBytes [uuidBytes]byte
	if _, err := rand.Read(randomBytes[:]); err != nil {
		return "", err
	}
	randomBytes[uuidVersionIndex] = randomBytes[uuidVersionIndex]&uuidVersionBits | uuidVersion4
	randomBytes[uuidVariantIndex] = randomBytes[uuidVariantIndex]&uuidVariantBits | uuidVariant10
	return fmt.Sprintf("%x-%x-%x-%x-%x", randomBytes[0:4], randomBytes[4:6], randomBytes[6:8], randomBytes[8:10], randomBytes[10:16]), nil
}

// TranscriptPath returns where Claude keeps the transcript of token for the
// working directory dir, under home.
func TranscriptPath(home, dir, token string) string {
	return filepath.Join(projectsDir(home), transcriptName(dir, token))
}

func projectsDir(home string) string {
	return filepath.Join(home, claudeDirName, projectsDirName)
}

func transcriptName(dir, token string) string {
	return filepath.Join(encodeDir(dir), token+transcriptExtension)
}

func encodeDir(dir string) string {
	return strings.Map(func(char rune) rune {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') {
			return char
		}
		return '-'
	}, dir)
}

func readLines(home, dir, token string) ([][]byte, error) {
	raw, err := readTranscript(home, transcriptName(dir, token))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w: no transcript at %s", ErrUnsupported, TranscriptPath(home, dir, token))
	}
	if err != nil {
		return nil, err
	}
	return splitLines(raw)
}

func readTranscript(home, name string) ([]byte, error) {
	root, err := os.OpenRoot(projectsDir(home))
	if err != nil {
		return nil, err
	}
	raw, readErr := root.ReadFile(name)
	return raw, errors.Join(readErr, root.Close())
}

func splitLines(raw []byte) ([][]byte, error) {
	var lines [][]byte
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 0, scannerInitialBuffer), scannerMaxLine)
	for scanner.Scan() {
		if line := bytes.TrimSpace(scanner.Bytes()); len(line) > 0 {
			lines = append(lines, append([]byte(nil), line...))
		}
	}
	return lines, scanner.Err()
}

func lineOf(lines [][]byte, position string) int {
	for index, line := range lines {
		if uuidOf(line) == position {
			return index
		}
	}
	return -1
}

func uuidOf(line []byte) string {
	var entry record
	if json.Unmarshal(line, &entry) != nil {
		return ""
	}
	return entry.UUID
}

func withSessionID(line []byte, sessionID string) ([]byte, error) {
	fields, isObject := jsonObject(line)
	if !isObject {
		return line, nil
	}
	if _, ok := fields[sessionIDKey]; !ok {
		return line, nil
	}
	encoded, err := json.Marshal(sessionID)
	if err != nil {
		return nil, err
	}
	fields[sessionIDKey] = encoded
	return json.Marshal(fields)
}

func jsonObject(line []byte) (map[string]json.RawMessage, bool) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(line, &fields) != nil {
		return nil, false
	}
	return fields, true
}
