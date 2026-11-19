package runner

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

func TestCommandErrorBoundsAndCleansHugeRemoteStderr(t *testing.T) {
	noisy := "\x1b[31mbanniere\x1b[0m \x1b]0;titre\x07" + strings.Repeat("é", 1<<16) + "\r\nPermission denied (publickey)\x1b[0m\n"
	msg := commandError(&exec.ExitError{Stderr: []byte(noisy)}).Error()

	if strings.ContainsAny(msg, "\x1b\a\r\n") {
		t.Fatalf("control characters survived: %q", msg)
	}
	if strings.Contains(msg, "titre") {
		t.Fatalf("an OSC sequence was kept: %q", msg)
	}
	if !strings.HasSuffix(msg, "Permission denied (publickey)") {
		t.Fatalf("the end of stderr was lost: %q", msg)
	}
	if !strings.Contains(msg, truncatedMark) {
		t.Fatalf("no truncation mark: %q", msg)
	}
	bounded, truncated := boundedStderr([]byte(noisy))
	if !truncated {
		t.Fatal("a 128 KiB stderr was not reported as truncated")
	}
	if len(bounded) > maxRemoteStderr+len(truncatedMark)+1 {
		t.Fatalf("stderr not bounded: %d bytes", len(bounded))
	}
	if !utf8.ValidString(msg) {
		t.Fatalf("the cut split a UTF-8 rune: %q", msg)
	}
}

func TestCommandErrorKeepsShortRemoteStderrWhole(t *testing.T) {
	msg := commandError(&exec.ExitError{Stderr: []byte("  fatal: hôte inconnu\n")}).Error()
	if !strings.HasSuffix(msg, ": fatal: hôte inconnu") {
		t.Fatalf("short stderr altered: %q", msg)
	}
	if _, truncated := boundedStderr([]byte("  fatal: hôte inconnu\n")); truncated {
		t.Fatal("short stderr reported as truncated")
	}
}

func TestBoundedStderrDropsUnterminatedSequencesAndBidiControls(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"unterminated CSI", "abc\x1b[31", "abc"},
		{"unterminated OSC", "x\x1b]0;titre", "x"},
		{"bidi controls", "a\u202eb\u2066c\u2069d", "abcd"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := boundedStderr([]byte(tc.raw))
			if got != tc.want {
				t.Fatalf("boundedStderr(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestRunnerSourcesCarryNoLocalizedText(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, char := range string(src) {
			if char > unicode.MaxASCII && unicode.IsLetter(char) {
				t.Fatalf("%s holds a non-ASCII letter %q: localized text belongs in the catalogue", name, char)
			}
		}
	}
}
