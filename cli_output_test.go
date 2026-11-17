package main

import (
	"context"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
)

type partialFailure struct {
	out string
}

func (p partialFailure) Error() string { return "seed interrompu" }

func (p partialFailure) PartialOutput() string { return p.out }

func TestPrintCLITextEscapesControlsAndKeepsTheLayout(t *testing.T) {
	var out strings.Builder
	if err := printCLIText(&out, "ligne 1\n\tligne 2 \x1b[31mrouge\x1b[0m"); err != nil {
		t.Fatalf("printCLIText: %v", err)
	}
	got := out.String()
	if strings.ContainsRune(got, '\x1b') {
		t.Fatalf("output carries a raw ESC: %q", got)
	}
	if !strings.Contains(got, "ligne 1\n\tligne 2 ") {
		t.Fatalf("output = %q, want the line break and the tab kept", got)
	}
}

func TestPrintCLITextEscapesBidiOverride(t *testing.T) {
	var out strings.Builder
	if err := printCLIText(&out, "fichier \u202egpj.exe"); err != nil {
		t.Fatalf("printCLIText: %v", err)
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

func TestRunPromptEscapesTheReply(t *testing.T) {
	d := &recordingDispatcher{reply: map[string]string{"session seed": "réponse \x1b[2Jeffacée\n- item"}}
	var out, errOut strings.Builder
	if err := runPromptWith(context.Background(), d, "n-a3fq", "x y", i18n.English, &out, &errOut); err != nil {
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
	d := &recordingDispatcher{fail: map[string]error{"session seed": partialFailure{out: "partiel \x1b]0;pwned\a"}}}
	var out, errOut strings.Builder
	err := runPromptWith(context.Background(), d, "n-a3fq", "x y", i18n.English, &out, &errOut)
	if err == nil {
		t.Fatal("runPromptWith = nil, want the seed failure")
	}
	if strings.ContainsAny(out.String(), "\x1b\a") {
		t.Fatalf("partial output carries a raw control: %q", out.String())
	}
	if !strings.Contains(out.String(), "partiel") {
		t.Fatalf("partial output = %q, want the text kept", out.String())
	}
}

func TestSeedFailureLineHasNoRawEscape(t *testing.T) {
	d := &recordingDispatcher{fail: map[string]error{"session seed": partialFailure{out: ""}}}
	var out, errOut strings.Builder
	err := runPromptWith(context.Background(), d, "n-a3fq\x1b[31m", "x y", i18n.English, &out, &errOut)
	if err == nil {
		t.Fatal("runPromptWith = nil, want the seed failure")
	}
	if line := cliErrorLine(err.Error()); strings.ContainsRune(line, '\x1b') {
		t.Fatalf("error line = %q, want no raw ESC", line)
	}
}
