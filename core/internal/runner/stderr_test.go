package runner

import (
	"os/exec"
	"strings"
	"testing"
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
	if !utf8.ValidString(msg) {
		t.Fatalf("the cut split a UTF-8 rune: %q", msg)
	}
	bounded := boundedStderr([]byte(noisy))
	if !strings.HasPrefix(bounded, truncatedMark+" ") {
		t.Fatalf("a 128 KiB stderr does not start with the cut mark: %q", bounded[:min(len(bounded), 16)])
	}
	if len(bounded) > maxRemoteStderr+len(truncatedMark)+1 {
		t.Fatalf("stderr not bounded: %d bytes", len(bounded))
	}
}

func TestCommandErrorKeepsShortRemoteStderrWhole(t *testing.T) {
	msg := commandError(&exec.ExitError{Stderr: []byte("  fatal: hôte inconnu\n")}).Error()
	if !strings.HasSuffix(msg, ": fatal: hôte inconnu") {
		t.Fatalf("short stderr altered: %q", msg)
	}
	if got := boundedStderr([]byte("  fatal: hôte inconnu\n")); got != "fatal: hôte inconnu" {
		t.Fatalf("short stderr = %q, want it unchanged", got)
	}
}

func TestBoundedStderrDropsUnterminatedSequencesAndBidiControls(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"unterminated CSI", "abc\x1b[31", "abc"},
		{"CSI alone leaves nothing visible", "\x1b[31", ""},
		{"unterminated CSI keeps its line", "\x1b[31 permission denied", "permission denied"},
		{"unterminated OSC", "x\x1b]0;titre", "x"},
		{"unterminated OSC stops at the first blank", "x\x1b]0;titre fin", "x fin"},
		{"terminated OSC does not cross a line", "x\x1b]0;titre\nerreur grave\x07 suite", "x erreur grave suite"},
		{"terminated OSC title with a blank is removed whole", "\x1b]0;mon titre\x07", ""},
		{"bidi controls", "a\u202eb\u2066c\u2069d", "abcd"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := boundedStderr([]byte(tc.raw)); got != tc.want {
				t.Fatalf("boundedStderr(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}
