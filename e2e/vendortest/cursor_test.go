package vendortest

import (
	"bytes"
	"strings"
	"testing"
)

func TestCursorRefusesAStreamJSONRunWithoutPrint(t *testing.T) {
	files := scriptFiles(t, "text reply text\n")
	var out, errOut bytes.Buffer
	code := Cursor([]string{"--output-format", "stream-json"}, Streams{In: strings.NewReader("hi"), Out: &out, Err: &errOut}, files)
	if code == 0 {
		t.Fatalf("the cursor fake answered without -p: %q", out.String())
	}
	if !strings.Contains(errOut.String(), errCursorPrint.Error()) {
		t.Fatalf("stderr = %q, want the -p refusal", errOut.String())
	}
	if out.Len() != 0 {
		t.Fatalf("the refused run wrote %q to stdout", out.String())
	}
}

func TestCursorAnswersAStreamJSONRunWithPrint(t *testing.T) {
	files := scriptFiles(t, "text reply text\n")
	var out, errOut bytes.Buffer
	args := []string{"-p", "--output-format", "stream-json"}
	code := Cursor(args, Streams{In: strings.NewReader("hi"), Out: &out, Err: &errOut}, files)
	if code != 0 {
		t.Fatalf("exit %d with stderr %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), `"text":"reply text"`) {
		t.Fatalf("stdout = %q, want the scripted reply", out.String())
	}
}

func TestCursorFailedTurnKeepsTheTextBeforeTheError(t *testing.T) {
	files := scriptFiles(t, "text partial answer\nerror boom\n")
	var out, errOut bytes.Buffer
	args := []string{"-p", "--output-format", "stream-json"}
	code := Cursor(args, Streams{In: strings.NewReader("hi"), Out: &out, Err: &errOut}, files)
	if code != 0 {
		t.Fatalf("exit %d with stderr %q", code, errOut.String())
	}
	text := strings.Index(out.String(), `"text":"partial answer"`)
	failed := strings.Index(out.String(), `"subtype":"error"`)
	if text < 0 || failed < text {
		t.Fatalf("stdout = %q, want the text before the error result", out.String())
	}
	if !strings.Contains(out.String(), `"is_error":true`) || !strings.Contains(out.String(), `"result":"boom"`) {
		t.Fatalf("stdout = %q, want the failure result", out.String())
	}
}
