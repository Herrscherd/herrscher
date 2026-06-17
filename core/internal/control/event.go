package control

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Event is one message on the session bus. The bridge emits turn events
// (human/status/chunk/reply) for a consumer to render; a consumer injects
// input/pick down to the bridge. One Event encodes to exactly one JSON line.
//
// chunk carries assistant prose; status carries a tool/progress line.
//
//	{"t":"human","who":"alice","text":"refactor the env loader"}
//	{"t":"status","text":"reading envfile.go"}
//	{"t":"chunk","text":"proposing 3 changes"}
//	{"t":"reply","text":"done","done":true}
//	{"t":"input","who":"terminal","text":"apply them"}
//	{"t":"pick","value":"2"}
//	{"t":"reset"}  // discard the in-progress turn (backend was reset mid-turn)
type Event struct {
	T     string `json:"t"`
	Who   string `json:"who,omitempty"`
	Text  string `json:"text,omitempty"`
	Value string `json:"value,omitempty"`
	Done  bool   `json:"done,omitempty"`
}

// WriteEvent encodes e as a single JSON line (newline-terminated). encoding/json
// escapes any newline inside a field, so one Event is always one line.
func WriteEvent(w io.Writer, e Event) error {
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = w.Write(b)
	return err
}

// ScanEvents reads JSON-line events from r, calling fn for each. A blank line is
// skipped; a line starting with '{' MUST parse as a JSON Event or it is a
// protocol error (returned, not surfaced as a pick); a non-'{' line is treated
// as the legacy bare pick value (one digit per connection) and surfaced as
// Event{T:"pick", Value:line} for back-compat with the pre-bus daemon. It
// returns the first error from fn, a malformed-line error, or a read error
// other than io.EOF.
func ScanEvents(r io.Reader, fn func(Event) error) error {
	sc := bufio.NewScanner(r)
	// 1 MiB line cap: a line longer than this ends the scan with bufio.ErrTooLong
	// (surfaced via the returned error). This is a deliberate protocol limit.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if line[0] == '{' {
			var e Event
			if err := json.Unmarshal([]byte(line), &e); err != nil {
				return fmt.Errorf("control: malformed event line: %w", err)
			}
			if err := fn(e); err != nil {
				return err
			}
			continue
		}
		// A non-JSON line is the legacy bare pick value (pre-bus daemon).
		if err := fn(Event{T: "pick", Value: line}); err != nil {
			return err
		}
	}
	return sc.Err()
}
