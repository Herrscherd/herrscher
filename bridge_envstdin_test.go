package main

import (
	"os"
	"strings"
	"testing"
)

// ssh carries no environment, and argv is forbidden for these values:
// /proc/<pid>/cmdline is world readable on both machines. Stdin is what is
// left, and the bridge never reads it otherwise.
func TestApplyEnvFromStdinSetsThePairs(t *testing.T) {
	t.Setenv("HS_TEST_URL", "")
	t.Setenv("HS_TEST_TOKEN", "")
	in := "HS_TEST_URL=https://gw\nHS_TEST_TOKEN=tok-supersecret\n\n"
	if err := applyEnvFromStdin(strings.NewReader(in)); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("HS_TEST_URL") != "https://gw" {
		t.Fatalf("url = %q", os.Getenv("HS_TEST_URL"))
	}
	if os.Getenv("HS_TEST_TOKEN") != "tok-supersecret" {
		t.Fatalf("token = %q", os.Getenv("HS_TEST_TOKEN"))
	}
}

// The blank line ends the block: whatever follows belongs to whoever reads
// stdin next, and must not be swallowed.
func TestApplyEnvFromStdinStopsAtTheBlankLine(t *testing.T) {
	t.Setenv("HS_TEST_AFTER", "")
	r := strings.NewReader("HS_TEST_A=1\n\nHS_TEST_AFTER=2\n")
	if err := applyEnvFromStdin(r); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("HS_TEST_AFTER") == "2" {
		t.Fatal("the reader consumed past the blank line")
	}
}

// A value can hold an '=' (base64 padding), and only the first one separates.
func TestApplyEnvFromStdinKeepsEqualsInValues(t *testing.T) {
	t.Setenv("HS_TEST_B64", "")
	if err := applyEnvFromStdin(strings.NewReader("HS_TEST_B64=YWJjZA==\n\n")); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("HS_TEST_B64") != "YWJjZA==" {
		t.Fatalf("value = %q", os.Getenv("HS_TEST_B64"))
	}
}

// EOF without a blank line is not an error: a caller that closed the pipe said
// everything it had to say.
func TestApplyEnvFromStdinToleratesEOF(t *testing.T) {
	t.Setenv("HS_TEST_EOF", "")
	if err := applyEnvFromStdin(strings.NewReader("HS_TEST_EOF=1\n")); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("HS_TEST_EOF") != "1" {
		t.Fatal("the last line before EOF was dropped")
	}
}
