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
	if !strings.Contains(msg, truncatedMark) {
		t.Fatalf("no truncation mark: %q", msg)
	}
	if bounded := boundedStderr([]byte(noisy)); len(bounded) > maxRemoteStderr+len(truncatedMark)+1 {
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
	if strings.Contains(msg, truncatedMark) {
		t.Fatalf("short stderr marked as truncated: %q", msg)
	}
}
