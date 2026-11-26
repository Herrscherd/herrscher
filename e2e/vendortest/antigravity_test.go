package vendortest

import (
	"bytes"
	"strings"
	"testing"
)

const antigravityTurnInput = `{"event":"user","message":{"content":"hi"}}` + "\n"

func antigravityTurnArgs() []string {
	return []string{"--input-format", "stream-json", "--output-format", "stream-json", "--dangerously-skip-permissions"}
}

func TestAntigravityListsModelsAsTabSeparatedLines(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Antigravity([]string{"models"}, Streams{In: strings.NewReader(""), Out: &out, Err: &errOut}, Files{}); code != 0 {
		t.Fatalf("exit %d with stderr %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "vendortest-model\tVendortest Model (High)") {
		t.Fatalf("stdout = %q, want an id and a label separated by a tab", out.String())
	}
}

func TestAntigravityAnswersAStreamJSONTurn(t *testing.T) {
	files := scriptFiles(t, "text reply text\n")
	var out, errOut bytes.Buffer
	if code := Antigravity(antigravityTurnArgs(), Streams{In: strings.NewReader(antigravityTurnInput), Out: &out, Err: &errOut}, files); code != 0 {
		t.Fatalf("exit %d with stderr %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), `"text_delta":"reply text"`) || !strings.Contains(out.String(), `"status":"SUCCESS"`) {
		t.Fatalf("stdout = %q, want the scripted reply and a success result", out.String())
	}
}

func TestAntigravityResumesTheConversationItWasGiven(t *testing.T) {
	files := scriptFiles(t, "text again\n")
	var out, errOut bytes.Buffer
	args := append(antigravityTurnArgs(), "--conversation", "conv-7")
	if code := Antigravity(args, Streams{In: strings.NewReader(antigravityTurnInput), Out: &out, Err: &errOut}, files); code != 0 {
		t.Fatalf("exit %d with stderr %q", code, errOut.String())
	}
	if strings.Count(out.String(), `"conversation_id":"conv-7"`) != 2 {
		t.Fatalf("stdout = %q, want the resumed conversation in init and result", out.String())
	}
}

func TestAntigravityFailedTurnEndsWithAnErrorResult(t *testing.T) {
	files := scriptFiles(t, "error boom\n")
	var out, errOut bytes.Buffer
	if code := Antigravity(antigravityTurnArgs(), Streams{In: strings.NewReader(antigravityTurnInput), Out: &out, Err: &errOut}, files); code != 0 {
		t.Fatalf("exit %d with stderr %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), `"status":"ERROR"`) || !strings.Contains(out.String(), `"error":"boom"`) {
		t.Fatalf("stdout = %q, want the failure result", out.String())
	}
}

func TestAntigravityRefusesAPromptOnArgv(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Antigravity([]string{"-p", "hi"}, Streams{In: strings.NewReader(""), Out: &out, Err: &errOut}, Files{}); code == 0 {
		t.Fatalf("the fake answered a prompt on argv: %q", out.String())
	}
	if !strings.Contains(errOut.String(), errAntigravityArgs.Error()) {
		t.Fatalf("stderr = %q, want the argument refusal", errOut.String())
	}
}
