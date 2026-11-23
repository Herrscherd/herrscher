package manage

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/clitext"
)

const claudeTrial = "claude -p " + trialPrompt

func (shell *fakeShell) call(t *testing.T, line string) setupCommand {
	t.Helper()
	shell.mu.Lock()
	defer shell.mu.Unlock()
	for _, call := range shell.calls {
		if strings.Join(append([]string{call.name}, call.args...), " ") == line {
			return call
		}
	}
	t.Fatalf("no call %q", line)
	return setupCommand{}
}

func installWithTrial(t *testing.T, reply fakeReply) *setupHarness {
	t.Helper()
	harness := newHarness(t, []byte(setupManifest), screenReview, nil)
	harness.shell.replies[claudeTrial] = reply
	harness.drive(harness.model.goTo(screenInstall))
	if !harness.model.install.done || harness.model.install.trialsRunning() {
		t.Fatalf("install = %+v, want done with the trials finished", harness.model.install)
	}
	return harness
}

func TestTrialAnswerFinishesTheInstall(t *testing.T) {
	harness := installWithTrial(t, fakeReply{out: "OK\n"})
	trials := harness.model.install.trials
	if len(trials) != 1 || trials[0].state != probePassed || harness.model.install.trialFailed() {
		t.Fatalf("trials = %+v, want one passed claude trial", trials)
	}
	call := harness.shell.call(t, claudeTrial)
	if call.dir == "" {
		t.Fatal("the trial ran in the current directory")
	}
	if _, err := os.Stat(call.dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("trial dir stat err = %v, want it removed", err)
	}
	view := harness.model.View()
	if strings.Contains(view, harness.text(clitext.SetupTrialFailed)) {
		t.Fatalf("view = %q, a passed trial reported a failure", view)
	}
}

func TestFailedTrialShowsTheRemedy(t *testing.T) {
	harness := installWithTrial(t, fakeReply{out: "Invalid API key\n", err: errProbeStatus})
	if !harness.model.install.trialFailed() || harness.model.install.err != nil || harness.model.install.restored {
		t.Fatalf("install = %+v, want a failed trial with the settings kept", harness.model.install)
	}
	view := harness.model.View()
	remedy := harness.model.trialRemedy(&harness.model.install.trials[0])
	for _, want := range []string{harness.text(clitext.SetupTrialFailed), "Invalid API key", remedy} {
		if !strings.Contains(view, want) {
			t.Errorf("view is missing %q", want)
		}
	}
	if !strings.HasSuffix(remedy, " setup "+kindClaude) {
		t.Fatalf("remedy = %q, want the claude setup command", remedy)
	}
}

func TestSilentTrialCountsAsFailed(t *testing.T) {
	harness := installWithTrial(t, fakeReply{out: "  \n"})
	if !harness.model.install.trialFailed() {
		t.Fatalf("trials = %+v, want an empty answer to fail", harness.model.install.trials)
	}
}

func TestMissingBackendCLISaysToInstallIt(t *testing.T) {
	harness := installWithTrial(t, fakeReply{err: exec.ErrNotFound})
	want := harness.model.text(clitext.SetupTrialMissing, clitext.Params{clitext.ParamName: kindClaude})
	if got := harness.model.trialRemedy(&harness.model.install.trials[0]); got != want {
		t.Fatalf("remedy = %q, want %q", got, want)
	}
}

func TestTrialPassesTheSavedKey(t *testing.T) {
	cases := []struct {
		name   string
		saved  map[string]string
		edited string
	}{
		{name: "typed in setup", edited: "sk-ant-typed"},
		{name: "already in the settings file", saved: map[string]string{envAnthropicKey: "sk-ant-typed"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			harness := newHarness(t, []byte(setupManifest), screenReview, tc.saved)
			if tc.edited != "" {
				harness.model.draft.set(envAnthropicKey, tc.edited)
			}
			harness.drive(harness.model.goTo(screenInstall))
			call := harness.shell.call(t, claudeTrial)
			if len(call.env) != 1 || call.env[0] != envAnthropicKey+"=sk-ant-typed" {
				t.Fatalf("env = %v, want the anthropic key the settings file holds", call.env)
			}
		})
	}
}

func TestSwapBinaryReplacesTheTarget(t *testing.T) {
	target := filepath.Join(t.TempDir(), binaryName)
	if err := errors.Join(os.WriteFile(target, []byte("old"), modeFile), os.WriteFile(freshBinary(target), []byte("new"), modeFile)); err != nil {
		t.Fatal(err)
	}
	if err := swapBinary(target); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "new" {
		t.Fatalf("target = %q (%v), want the new build", data, err)
	}
	for _, leftover := range []string{freshBinary(target), target + staleSuffix} {
		if _, err := os.Stat(leftover); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s stat err = %v, want it gone", leftover, err)
		}
	}
}

func TestSwapBinaryWithoutAFreshBuildKeepsTheTarget(t *testing.T) {
	target := filepath.Join(t.TempDir(), binaryName)
	if err := os.WriteFile(target, []byte("old"), modeFile); err != nil {
		t.Fatal(err)
	}
	if err := swapBinary(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want the missing fresh build reported", err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "old" {
		t.Fatalf("target = %q (%v), want the old binary put back", data, err)
	}
}

func TestSwapBinaryInstallsWhenNothingWasThere(t *testing.T) {
	target := filepath.Join(t.TempDir(), binaryName)
	if err := os.WriteFile(freshBinary(target), []byte("new"), modeFile); err != nil {
		t.Fatal(err)
	}
	if err := swapBinary(target); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "new" {
		t.Fatalf("target = %q (%v), want the new build", data, err)
	}
}
