package control

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// WriteEvent encodes e as a single JSON line (newline-terminated). encoding/json
// escapes any newline inside a field, so one Event is always one line.
func WriteEvent(dst io.Writer, e contracts.Event) error {
	payload, err := json.Marshal(e)
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	_, err = dst.Write(payload)
	return err
}

const (
	scanInitialBuffer = 64 * 1024
	scanMaxLine       = 1024 * 1024
)

// ScanEvents decodes events from r and passes each one to fn until r ends or fn fails.
// ScanEvents passes each event read from r to handle and rejects a malformed line.
func ScanEvents(r io.Reader, handle func(contracts.Event) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, scanInitialBuffer), scanMaxLine)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if line[0] == '{' {
			var event contracts.Event
			if err := json.Unmarshal([]byte(line), &event); err != nil {
				return fmt.Errorf("control: malformed event line: %w", err)
			}
			if err := handle(event); err != nil {
				return err
			}
			continue
		}
		if err := handle(contracts.Event{T: "pick", Value: line}); err != nil {
			return err
		}
	}
	return scanner.Err()
}
