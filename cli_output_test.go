package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/termpaint"
)

type partialFailureError struct {
	out string
}

func (p partialFailureError) Error() string { return "seed interrompu" }

func (p partialFailureError) PartialOutput() string { return p.out }

func TestWriteCLITextEscapesControlsAndKeepsTheLayout(t *testing.T) {
	var out strings.Builder
	if err := writeCLIText(&out, "ligne 1\n\tligne 2 \x1b[31mrouge\x1b[0m"); err != nil {
		t.Fatalf("writeCLIText: %v", err)
	}
	got := out.String()
	if strings.ContainsRune(got, '\x1b') {
		t.Fatalf("output carries a raw ESC: %q", got)
	}
	if !strings.Contains(got, "ligne 1\n\tligne 2 ") {
		t.Fatalf("output = %q, want the line break and the tab kept", got)
	}
}

func TestWriteCLITextEscapesBidiOverride(t *testing.T) {
	var out strings.Builder
	if err := writeCLIText(&out, "fichier \u202egpj.exe"); err != nil {
		t.Fatalf("writeCLIText: %v", err)
	}
	if strings.ContainsRune(out.String(), '\u202e') {
		t.Fatalf("output carries a raw U+202E: %q", out.String())
	}
}

func TestCLIErrorLineEscapesTheMessage(t *testing.T) {
	got := cliErrorLine("erreur \x1b]0;titre\a\nsuite")
	if strings.ContainsAny(got, "\x1b\a\n") {
		t.Fatalf("error line = %q, want no raw control", got)
	}
}

func TestEscapedParamEscapesTheValue(t *testing.T) {
	hostile := "passerelle \x1b[31mrouge\x1b[0m\a"
	got := escapedParam(clitext.ParamFailure, hostile)[clitext.ParamFailure]
	if got != termpaint.Escape(hostile).String() {
		t.Fatalf("param = %v, want the escaped text", got)
	}
}

func TestRunPromptEscapesTheReply(t *testing.T) {
	d := &recordingDispatcher{reply: map[string]string{"session seed": "réponse \x1b[2Jeffacée\n- item"}}
	var out, errOut strings.Builder
	if err := runPromptWith(context.Background(), testMessages(), d, "n-a3fq", "x y", i18n.English, &out, &errOut); err != nil {
		t.Fatalf("runPromptWith: %v", err)
	}
	if strings.ContainsRune(out.String(), '\x1b') {
		t.Fatalf("stdout carries a raw ESC: %q", out.String())
	}
	if !strings.Contains(out.String(), "\n- item") {
		t.Fatalf("stdout = %q, want the list layout kept", out.String())
	}
}

func TestRunPromptEscapesThePartialOutput(t *testing.T) {
	d := &recordingDispatcher{fail: map[string]error{"session seed": partialFailureError{out: "fragment \x1b]0;pwned\a"}}}
	var out, errOut strings.Builder
	err := runPromptWith(context.Background(), testMessages(), d, "n-a3fq", "x y", i18n.English, &out, &errOut)
	if err == nil {
		t.Fatal("runPromptWith = nil, want the seed failure")
	}
	if strings.ContainsAny(out.String(), "\x1b\a") {
		t.Fatalf("partial output carries a raw control: %q", out.String())
	}
	if !strings.Contains(out.String(), "fragment") {
		t.Fatalf("partial output = %q, want the text kept", out.String())
	}
}

func TestReportErrorEscapesTheSeedFailure(t *testing.T) {
	seedErr := errors.New("graine \x1b]0;pwned\a refusée")
	d := &recordingDispatcher{fail: map[string]error{"session seed": seedErr}}
	var out, errOut strings.Builder
	err := runPromptWith(context.Background(), testMessages(), d, "n-a3fq", "x y", i18n.English, &out, &errOut)
	if err == nil {
		t.Fatal("runPromptWith = nil, want the seed failure")
	}
	var stderr strings.Builder
	if code := reportErrorTo(testMessages(), &stderr, err); code != 1 {
		t.Fatalf("reportErrorTo = %d, want 1", code)
	}
	line := stderr.String()
	if strings.ContainsAny(line, "\x1b\a") {
		t.Fatalf("stderr = %q, want no raw control", line)
	}
	if !strings.Contains(line, "n-a3fq") {
		t.Fatalf("stderr = %q, want the session name", line)
	}
}
