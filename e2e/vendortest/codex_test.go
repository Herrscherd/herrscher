package vendortest

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func decodeRPCLines(t *testing.T, text string) []map[string]any {
	t.Helper()
	var messages []map[string]any
	for line := range strings.Lines(text) {
		var message map[string]any
		if err := json.Unmarshal([]byte(line), &message); err != nil {
			t.Fatalf("the codex fake wrote %q, which is not JSON: %v", line, err)
		}
		messages = append(messages, message)
	}
	return messages
}

func objectField(t *testing.T, parent map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := parent[key].(map[string]any)
	if !ok {
		t.Fatalf("%s is missing or not an object in %v", key, parent)
	}
	return value
}

func TestCodexSendsTheFinalTextAsAnAgentMessageItemBeforeTurnCompleted(t *testing.T) {
	files := scriptFiles(t, "text reply text\n")
	request := `{"id":7,"method":"turn/start","params":{"threadId":"thread-1","input":[{"type":"text","text":"hi"}]}}` + "\n"
	var out, errOut bytes.Buffer
	code := Codex([]string{appServerVerb}, Streams{In: strings.NewReader(request), Out: &out, Err: &errOut}, files)
	if code != 0 {
		t.Fatalf("exit %d with stderr %q", code, errOut.String())
	}

	var completed map[string]any
	completedAt, turnDoneAt := -1, -1
	for index, message := range decodeRPCLines(t, out.String()) {
		switch message["method"] {
		case methodItemCompleted:
			completed = message
			completedAt = index
		case "turn/completed":
			turnDoneAt = index
		}
	}
	if completed == nil {
		t.Fatalf("no %s notification in %q", methodItemCompleted, out.String())
	}
	if turnDoneAt < 0 || completedAt > turnDoneAt {
		t.Fatalf("%s at %d must come before turn/completed at %d", methodItemCompleted, completedAt, turnDoneAt)
	}

	params := objectField(t, completed, "params")
	item := objectField(t, params, keyItem)
	if item[keyType] != wordAgent {
		t.Fatalf("item type = %v, want %s", item[keyType], wordAgent)
	}
	if item[keyText] != "reply text" {
		t.Fatalf("item text = %v, want the full reply", item[keyText])
	}
	if id, ok := item["id"].(string); !ok || id == "" {
		t.Fatalf("item carries no id: %v", item)
	}
	if params[keyThreadID] != "thread-1" || params[keyTurnID] != codexTurnID {
		t.Fatalf("item/completed params = %v, want the thread and turn ids", params)
	}
}

func TestCodexFailedTurnKeepsTheTextBeforeTheFailure(t *testing.T) {
	files := scriptFiles(t, "text partial answer\nerror boom\n")
	request := `{"id":7,"method":"turn/start","params":{"threadId":"thread-1","input":[{"type":"text","text":"hi"}]}}` + "\n"
	var out, errOut bytes.Buffer
	code := Codex([]string{appServerVerb}, Streams{In: strings.NewReader(request), Out: &out, Err: &errOut}, files)
	if code != 0 {
		t.Fatalf("exit %d with stderr %q", code, errOut.String())
	}

	itemAt, failedAt := -1, -1
	var failedTurn map[string]any
	for index, message := range decodeRPCLines(t, out.String()) {
		switch message["method"] {
		case methodItemCompleted:
			itemAt = index
			item := objectField(t, objectField(t, message, "params"), keyItem)
			if item[keyText] != "partial answer" {
				t.Fatalf("item text = %v, want the text spoken before the failure", item[keyText])
			}
		case methodTurnCompleted:
			failedAt = index
			failedTurn = objectField(t, objectField(t, message, "params"), keyTurn)
		}
	}
	if itemAt < 0 || failedAt < itemAt {
		t.Fatalf("%s at %d must come before turn/completed at %d", methodItemCompleted, itemAt, failedAt)
	}
	if failedTurn[keyStatus] != valueFailed {
		t.Fatalf("turn status = %v, want %s", failedTurn[keyStatus], valueFailed)
	}
	if objectField(t, failedTurn, keyError)[keyMessage] != "boom" {
		t.Fatalf("turn error = %v, want the failure message", failedTurn[keyError])
	}
}
