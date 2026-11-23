package manage

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/termpaint"
)

func colouredStyle() style {
	return style{
		reset: "\x1b[0m",
		bold:  "\x1b[1m",
		dim:   "\x1b[2m",
		green: "\x1b[32m",
		red:   "\x1b[31m",
	}
}

type captureResult struct {
	text string
	err  error
}

func captureStderr(t *testing.T, run func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	saved := os.Stderr
	os.Stderr = writer
	defer func() {
		os.Stderr = saved
		closePipeEnd(t, writer)
		closePipeEnd(t, reader)
	}()
	captured := make(chan captureResult, 1)
	go func() {
		data, readErr := io.ReadAll(reader)
		captured <- captureResult{text: string(data), err: readErr}
	}()
	run()
	os.Stderr = saved
	if err := writer.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}
	result := <-captured
	if result.err != nil {
		t.Fatalf("read stderr: %v", result.err)
	}
	return result.text
}

func closePipeEnd(t *testing.T, closer io.Closer) {
	t.Helper()
	if err := closer.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		t.Errorf("close pipe end: %v", err)
	}
}

func declining() *bufio.Reader {
	return bufio.NewReader(strings.NewReader("n\n"))
}

func TestWarnListsEscapedFindingsWithoutColour(t *testing.T) {
	hostile := "risque \x1b]0;pwn\a"
	out := captureStderr(t, func() {
		newPromptDecider(testMessages(), declining(), style{}).Warn(context.Background(), []string{hostile})
	})
	if strings.Contains(out, "\x1b]") || strings.Contains(out, "\a") {
		t.Fatalf("output = %q, want no raw control from the finding", out)
	}
	if !strings.Contains(out, termpaint.Escape(hostile).String()) {
		t.Fatalf("output = %q, want the escaped finding", out)
	}
}

func TestWarnKeepsColourAroundEscapedFinding(t *testing.T) {
	hostile := "risque \x1b]0;pwn\a"
	out := captureStderr(t, func() {
		newPromptDecider(testMessages(), declining(), colouredStyle()).Warn(context.Background(), []string{hostile})
	})
	if !strings.Contains(out, "\x1b[31m!\x1b[0m") {
		t.Fatalf("output = %q, want the red marker kept", out)
	}
	if strings.Contains(out, "\x1b]") || strings.Contains(out, "\a") {
		t.Fatalf("output = %q, want no raw control from the finding", out)
	}
}

func TestFailedEscapesBuildOutputLineByLine(t *testing.T) {
	build := "compile ok\n\x1b]0;pwn\a\nfin\n"
	out := captureStderr(t, func() {
		newPromptDecider(testMessages(), declining(), colouredStyle()).Failed(context.Background(), build)
	})
	if strings.Contains(out, "\x1b]") || strings.Contains(out, "\a") {
		t.Fatalf("output = %q, want no raw control from the build output", out)
	}
	if !strings.Contains(out, "fin") || !strings.Contains(out, "compile ok") {
		t.Fatalf("output = %q, want each build line kept", out)
	}
}

func TestFailedShowsBuildOutputWithoutColour(t *testing.T) {
	out := captureStderr(t, func() {
		newPromptDecider(testMessages(), declining(), style{}).Failed(context.Background(), "ligne 1\nligne 2")
	})
	if strings.Contains(out, "\x1b") {
		t.Fatalf("plain output carries an escape: %q", out)
	}
	if !strings.Contains(out, "    ligne 1\n") || !strings.Contains(out, "    ligne 2\n") {
		t.Fatalf("output = %q, want each line indented", out)
	}
}
