package manage

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/internal/errwriter"
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
	trial := harness.model.install.trials[0]
	remedy := trialRemedy(harness.model.messages, trial.spec, trial.err)
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
	trial := harness.model.install.trials[0]
	if got := trialRemedy(harness.model.messages, trial.spec, trial.err); got != want {
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
			if slices.ContainsFunc(call.args, func(arg string) bool { return strings.Contains(arg, "sk-ant") }) {
				t.Fatalf("args = %v, the key reached the command line", call.args)
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
	if _, err := os.Stat(freshBinary(target)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("fresh build stat err = %v, want it moved", err)
	}
	if data, err := os.ReadFile(staleBinary(target)); err != nil || string(data) != "old" {
		t.Fatalf("stale = %q (%v), want the previous binary kept beside it", data, err)
	}
	if err := os.WriteFile(freshBinary(target), []byte("newer"), modeFile); err != nil {
		t.Fatal(err)
	}
	if err := swapBinary(target); err != nil {
		t.Fatalf("second swap over a kept stale binary: %v", err)
	}
	if data, err := os.ReadFile(staleBinary(target)); err != nil || string(data) != "new" {
		t.Fatalf("stale = %q (%v), want only the last previous binary", data, err)
	}
}

func TestSwapBinaryNeverPutsBackAnOlderStaleBinary(t *testing.T) {
	target := filepath.Join(t.TempDir(), binaryName)
	if err := os.WriteFile(staleBinary(target), []byte("ancient"), modeFile); err != nil {
		t.Fatal(err)
	}
	if err := swapBinary(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want the missing fresh build reported", err)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("target stat err = %v, want nothing put in place", err)
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

func (harness *setupHarness) cancellableInstall(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	run := harness.model.install
	run.ctx, run.cancel = ctx, cancel
	harness.model.install = run
}

func (harness *setupHarness) blockTrials(t *testing.T) (tea.Cmd, chan struct{}, chan struct{}) {
	t.Helper()
	installWithTrialDone := harness.model.install.done && !harness.model.install.trialsRunning()
	if !installWithTrialDone {
		t.Fatalf("install = %+v, want done before blocking a trial", harness.model.install)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	harness.shell.effect = func(cmd setupCommand) {
		if cmd.name == kindClaude {
			close(entered)
			<-release
		}
	}
	harness.cancellableInstall(t)
	return harness.model.startTrials(), entered, release
}

func TestLeavingDuringATrialWaitsForIt(t *testing.T) {
	harness := installWithTrial(t, fakeReply{out: "OK\n"})
	pending, entered, release := harness.blockTrials(t)
	finished := make(chan struct{})
	go func() {
		pending()
		close(finished)
	}()
	<-entered
	harness.press(tea.KeyMsg{Type: tea.KeyEsc})
	harness.expectScreen(t, screenInstall)
	abandoned := make(chan struct{})
	go func() {
		harness.model.abandon()
		close(abandoned)
	}()
	select {
	case <-abandoned:
		close(release)
		<-finished
		t.Fatal("abandon returned while the trial was still running")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	<-abandoned
	<-finished
	if harness.model.install.trialsAnswered() {
		t.Fatal("a trial cut short counts as answered, setup would exit 0")
	}
}

func TestCtrlCDuringATrialQuitsWithoutARemedy(t *testing.T) {
	harness := installWithTrial(t, fakeReply{out: "OK\n"})
	harness.shell.replies[claudeTrial] = fakeReply{err: context.Canceled}
	harness.cancellableInstall(t)
	pending := harness.model.startTrials()
	if _, cmd := harness.model.Update(tea.KeyMsg{Type: tea.KeyCtrlC}); cmd != nil {
		t.Fatal("ctrl+c quit while a trial was still running")
	}
	_, cmd := harness.model.Update(pending())
	if cmd == nil {
		t.Fatal("the aborted trial never quits")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("the aborted trial returns a command that is not a quit")
	}
	if harness.model.install.trialFailed() || harness.model.install.trialsAnswered() {
		t.Fatalf("trials = %+v, want the aborted trial neither failed nor answered", harness.model.install.trials)
	}
}

func TestTrialPastItsDeadlineFails(t *testing.T) {
	harness := installWithTrial(t, fakeReply{out: "OK\n"})
	harness.model.deps.trialWait = 10 * time.Millisecond
	harness.model.deps.run = func(ctx context.Context, _ setupCommand) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	harness.cancellableInstall(t)
	harness.drive(harness.model.startTrials())
	trial := harness.model.install.trials[0]
	if trial.state != probeFailed || !errors.Is(trial.err, context.DeadlineExceeded) {
		t.Fatalf("trial = %+v, want failed on its deadline", trial)
	}
}

func fakeVendor(t *testing.T, name, script string) {
	t.Helper()
	dir := t.TempDir()
	if script != "" {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script+"\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
}

func TestInitTrialReportsEachBackend(t *testing.T) {
	stack := []string{catalog()[catBackend][kindClaude]}
	cases := []struct {
		name   string
		script string
		params clitext.Params
		want   clitext.Key
		code   int
	}{
		{name: "answers", script: "echo OK", code: 0},
		{name: "silent", script: "exit 1", code: 1, want: clitext.SetupTrialRemedy, params: clitext.Params{clitext.ParamCommand: commandName(invokedBinary(os.Args)) + " setup " + kindClaude}},
		{name: "missing", code: 1, want: clitext.SetupTrialMissing, params: clitext.Params{clitext.ParamName: kindClaude}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fakeVendor(t, kindClaude, tc.script)
			var out, errs bytes.Buffer
			code := trialStack(t.Context(), testMessages(), errwriter.New(&out), errwriter.New(&errs), stack)
			if code != tc.code {
				t.Fatalf("code = %d, want %d; stderr %q", code, tc.code, errs.String())
			}
			if tc.want == "" {
				if errs.Len() != 0 || !strings.Contains(out.String(), "✓") {
					t.Fatalf("stdout %q, stderr %q, want one passed trial", out.String(), errs.String())
				}
				return
			}
			want := msg(testMessages(), tc.want, tc.params)
			if !strings.Contains(errs.String(), want) {
				t.Fatalf("stderr %q, want %q", errs.String(), want)
			}
		})
	}
}

func TestLeavingGivesUpOnATrialThatKeepsRunning(t *testing.T) {
	harness := installWithTrial(t, fakeReply{out: "OK\n"})
	harness.model.deps.abandonWait = 20 * time.Millisecond
	pending, entered, release := harness.blockTrials(t)
	finished := make(chan struct{})
	go func() {
		pending()
		close(finished)
	}()
	<-entered
	harness.model.abandon()
	close(release)
	<-finished
	want := msg(testMessages(), clitext.SetupTrialNotStopped, nil)
	if harness.model.install.cleanupErr == nil || !strings.Contains(harness.model.install.cleanupErr.Error(), want) {
		t.Fatalf("cleanupErr = %v, want %q", harness.model.install.cleanupErr, want)
	}
}

func TestInitTrialStopsOnceCancelled(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	fakeVendor(t, kindClaude, ": > "+marker+"\necho OK")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var out, errs bytes.Buffer
	code := trialStack(ctx, testMessages(), errwriter.New(&out), errwriter.New(&errs), []string{catalog()[catBackend][kindClaude]})
	if code != 1 {
		t.Fatalf("code = %d, want 1 after a cancel", code)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("marker stat err = %v, want no backend started after a cancel", err)
	}
	if strings.Contains(errs.String(), "✗") {
		t.Fatalf("stderr %q, want no failure line per backend after a cancel", errs.String())
	}
}
