package manage

import (
	"bufio"
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/termpaint"
)

var colouredStyle = style{
	reset: "\x1b[0m",
	bold:  "\x1b[1m",
	dim:   "\x1b[2m",
	green: "\x1b[32m",
	red:   "\x1b[31m",
}

func captureStderr(t *testing.T, run func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer reader.Close()
	saved := os.Stderr
	os.Stderr = writer
	defer func() { os.Stderr = saved }()
	captured := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(reader)
		captured <- string(data)
	}()
	run()
	os.Stderr = saved
	if err := writer.Close(); err != nil {
		t.Fatalf("close pipe: %v", err)
	}
	return <-captured
}

func answering(line string) *bufio.Reader {
	return bufio.NewReader(strings.NewReader(line))
}

func TestChooseKindListsPlainKindsWithoutColour(t *testing.T) {
	var choice string
	out := captureStderr(t, func() {
		var err error
		choice, err = chooseKind(style{}, answering("\n"), catGateway)
		if err != nil {
			t.Errorf("chooseKind: %v", err)
		}
	})
	if choice != defaultStack()[catGateway] {
		t.Fatalf("choice = %q, want the default %q", choice, defaultStack()[catGateway])
	}
	if strings.Contains(out, "\x1b") {
		t.Fatalf("plain output carries an escape: %q", out)
	}
	if !strings.Contains(out, kindDiscord) {
		t.Fatalf("output = %q, want the kind listed", out)
	}
}

func TestChooseKindColoursTheDefaultKind(t *testing.T) {
	out := captureStderr(t, func() {
		if _, err := chooseKind(colouredStyle, answering("\n"), catGateway); err != nil {
			t.Errorf("chooseKind: %v", err)
		}
	})
	want := "\x1b[32m" + kindDiscord + "\x1b[0m"
	if !strings.Contains(out, want) {
		t.Fatalf("output = %q, want the default kind wrapped in green", out)
	}
	if strings.Contains(out, `\x1b`) {
		t.Fatalf("output = %q, want no escaped ESC printed as text", out)
	}
}

func TestChooseKindEscapesHostileCategory(t *testing.T) {
	hostile := "gateway\x1b]0;pwn\a"
	out := captureStderr(t, func() {
		if _, err := chooseKind(colouredStyle, answering("\n"), hostile); err != nil {
			t.Errorf("chooseKind: %v", err)
		}
	})
	if strings.Contains(out, "\x1b]") || strings.Contains(out, "\a") {
		t.Fatalf("output = %q, want no raw control from the category", out)
	}
	if !strings.Contains(out, termpaint.Escape(hostile).String()) {
		t.Fatalf("output = %q, want the escaped category", out)
	}
}

func TestWarnListsEscapedFindingsWithoutColour(t *testing.T) {
	hostile := "risque \x1b]0;pwn\a"
	out := captureStderr(t, func() {
		newPromptDecider(answering("n\n"), style{}).Warn(context.Background(), []string{hostile})
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
		newPromptDecider(answering("n\n"), colouredStyle).Warn(context.Background(), []string{hostile})
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
		newPromptDecider(answering("n\n"), colouredStyle).Failed(context.Background(), build)
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
		newPromptDecider(answering("n\n"), style{}).Failed(context.Background(), "ligne 1\nligne 2")
	})
	if strings.Contains(out, "\x1b") {
		t.Fatalf("plain output carries an escape: %q", out)
	}
	if !strings.Contains(out, "    ligne 1\n") || !strings.Contains(out, "    ligne 2\n") {
		t.Fatalf("output = %q, want each line indented", out)
	}
}
