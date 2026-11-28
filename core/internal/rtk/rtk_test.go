package rtk

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func fakeRTK(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), Binary)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("write fake rtk: %v", err)
	}
	return path
}

func writeSettings(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	return path
}

func absentLook(string) (string, error) { return "", errors.New("not found") }

func TestProbeAbsentWhenNotOnPath(t *testing.T) {
	status := Probe(context.Background(), absentLook)
	if status.Present || status.Version != "" || status.UserHook {
		t.Fatalf("absent rtk probed as %+v", status)
	}
}

func TestProbeReadsTheVersionOfTheBinaryFound(t *testing.T) {
	path := fakeRTK(t, `echo "rtk 1.0.1"`)
	status := Probe(context.Background(), func(string) (string, error) { return path, nil })
	if !status.Present || status.Version != "1.0.1" {
		t.Fatalf("probed as %+v, want present at 1.0.1", status)
	}
}

func TestProbeKeepsPresenceWhenVersionIsUnreadable(t *testing.T) {
	path := fakeRTK(t, `echo "something else"; exit 1`)
	status := Probe(context.Background(), func(string) (string, error) { return path, nil })
	if !status.Present || status.Version != "" {
		t.Fatalf("probed as %+v, want present with no version", status)
	}
}

func TestProbeSeesTheUserHookWhenRTKIsAbsent(t *testing.T) {
	settings := writeSettings(t, `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"rtk hook claude"}]}]}}`)
	status := Probe(context.Background(), absentLook, settings)
	if status.Present || !status.UserHook {
		t.Fatalf("probed as %+v, want absent with the user hook seen", status)
	}
}

func TestParseVersion(t *testing.T) {
	cases := map[string]string{
		"rtk 1.0.1\n": "1.0.1",
		"rtk 2.3":     "2.3",
		"":            "",
		"rtk":         "",
		"tool 1.0.1":  "",
		"rtk 1 2":     "",
	}
	for in, want := range cases {
		if got := ParseVersion(in); got != want {
			t.Errorf("ParseVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHasUserHookRecognisesAnRTKHookOnly(t *testing.T) {
	cases := map[string]bool{
		`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"command":"rtk hook claude"}]}]}}`:                  true,
		`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"command":"/usr/bin/rtk-rewrite.sh"}]}]}}`:          true,
		`{"hooks":{"PreToolUse":[{"matcher":"*","hooks":[{"command":"'/bin/herrscher' approve hook --rtk"}]}]}}`: false,
		`{"hooks":{"PostToolUse":[{"matcher":"Bash","hooks":[{"command":"rtk hook claude"}]}]}}`:                 false,
		`{"permissions":{"allow":["Bash(rtk *)"]}}`:                                                              false,
		`{not json`: false,
	}
	for body, want := range cases {
		if got := HasUserHook(writeSettings(t, body)); got != want {
			t.Errorf("HasUserHook(%s) = %v, want %v", body, got, want)
		}
	}
}

func TestHasUserHookIsFalseWithoutFiles(t *testing.T) {
	if HasUserHook(filepath.Join(t.TempDir(), "missing.json")) {
		t.Fatal("a missing settings file reported a user hook")
	}
}

func TestRewriteAddsThePrefixToASimpleCommand(t *testing.T) {
	path := fakeRTK(t, `printf 'rtk %s' "$2"`)
	got, ok := Rewrite(context.Background(), path, "git status")
	if !ok || got != "rtk git status" {
		t.Fatalf("Rewrite = %q, %v; want rtk git status, true", got, ok)
	}
}

func TestRewriteAcceptsTheRewrittenExit(t *testing.T) {
	path := fakeRTK(t, `printf 'rtk %s' "$2"; exit 3`)
	got, ok := Rewrite(context.Background(), path, "git log -1")
	if !ok || got != "rtk git log -1" {
		t.Fatalf("Rewrite = %q, %v; want rtk git log -1, true", got, ok)
	}
}

func TestRewriteFallsBackWhenAClaudeDenyRuleMatches(t *testing.T) {
	path := fakeRTK(t, `printf 'rtk %s' "$2"; exit 2`)
	if got, ok := Rewrite(context.Background(), path, "git status"); ok {
		t.Fatalf("a deny rule rewrote to %q, the command must run as written", got)
	}
}

func TestRewriteRefusesAStructuralChange(t *testing.T) {
	path := fakeRTK(t, `printf 'rtk git status'`)
	if got, ok := Rewrite(context.Background(), path, "git status && rm -rf /tmp/x"); ok {
		t.Fatalf("Rewrite accepted a rewrite that is not a prefix: %q", got)
	}
}

func TestRewriteRefusesAChainRewrittenInsideItself(t *testing.T) {
	path := fakeRTK(t, `printf 'rtk git status && rtk ls'`)
	if got, ok := Rewrite(context.Background(), path, "git status && ls"); ok {
		t.Fatalf("Rewrite accepted an inner rewrite: %q", got)
	}
}

func TestRewriteFailsOpenOnNoRewriteExits(t *testing.T) {
	for _, exit := range []string{"1", "2"} {
		path := fakeRTK(t, `printf 'rtk %s' "$2"; exit `+exit)
		if got, ok := Rewrite(context.Background(), path, "git status"); ok {
			t.Fatalf("exit %s rewrote to %q", exit, got)
		}
	}
}

func TestRewriteFailsOpenWhenTheBinaryIsMissing(t *testing.T) {
	if got, ok := Rewrite(context.Background(), filepath.Join(t.TempDir(), "rtk"), "git status"); ok {
		t.Fatalf("a missing binary rewrote to %q", got)
	}
}

func TestRewriteNeverRewritesAnEmptyCommand(t *testing.T) {
	path := fakeRTK(t, `printf 'rtk '`)
	if got, ok := Rewrite(context.Background(), path, ""); ok {
		t.Fatalf("an empty command rewrote to %q", got)
	}
}
