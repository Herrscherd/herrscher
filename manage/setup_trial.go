package manage

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Herrscherd/herrscher/core/clitext"
)

const (
	trialTimeout = 3 * time.Minute
	trialPrompt  = "Reply with the single word OK and nothing else."
	trialDirName = "herrscher-setup-trial-*"
)

type trialSpec struct {
	module  setupModule
	command setupCommand
	section string
}

type trialDoneMsg struct {
	err    error
	output string
	index  int
}

type trialStep struct {
	spec   trialSpec
	label  string
	output string
	err    error
	state  probeState
}

func backendTrials(chosen moduleSet, value func(string) string) []trialSpec {
	specs := []trialSpec{
		{module: moduleClaude, section: kindClaude, command: setupCommand{name: kindClaude, args: []string{"-p", trialPrompt}, env: presentEnv(value, envAnthropicKey)}},
		{module: moduleCodex, section: kindCodex, command: setupCommand{name: kindCodex, args: []string{"exec", "--skip-git-repo-check", trialPrompt}}},
		{module: moduleCursor, section: kindCursor, command: setupCommand{name: cursorCLI, args: []string{"-p", trialPrompt}, env: presentEnv(value, envCursorKey)}},
	}
	picked := make([]trialSpec, 0, len(specs))
	for _, spec := range specs {
		if chosen[spec.module] {
			picked = append(picked, spec)
		}
	}
	return picked
}

func presentEnv(value func(string) string, key string) []string {
	if found := value(key); found != "" {
		return []string{key + "=" + found}
	}
	return nil
}

func trialLabel(messages *clitext.Messages, spec trialSpec) string {
	return msg(messages, clitext.SetupTrialStep, clitext.Params{clitext.ParamName: spec.command.name})
}

func runTrialIn(parent context.Context, runner commandRunner, command setupCommand, limit time.Duration) ([]byte, error) {
	dir, err := os.MkdirTemp("", trialDirName)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(parent, limit)
	defer cancel()
	command.dir = dir
	out, err := runner(ctx, command)
	return out, errors.Join(err, os.RemoveAll(dir))
}

func trialAnswered(output string, err error) bool {
	return err == nil && strings.TrimSpace(output) != ""
}

func trialFailureLines(messages *clitext.Messages, spec trialSpec, output string, err error) []string {
	var lines []string
	if err != nil && !errors.Is(err, exec.ErrNotFound) {
		lines = append(lines, err.Error())
	}
	for _, out := range strings.Split(output, "\n") {
		if out != "" {
			lines = append(lines, out)
		}
	}
	return append(lines, trialRemedy(messages, spec, err))
}

func trialRemedy(messages *clitext.Messages, spec trialSpec, err error) string {
	if errors.Is(err, exec.ErrNotFound) {
		return msg(messages, clitext.SetupTrialMissing, clitext.Params{clitext.ParamName: spec.command.name})
	}
	return msg(messages, clitext.SetupTrialRemedy, clitext.Params{clitext.ParamCommand: commandName(invokedBinary(os.Args)) + " setup " + spec.section})
}

func (model *setupModel) startTrials() tea.Cmd {
	model.install.trials = nil
	for _, spec := range backendTrials(model.modules, model.draft.value) {
		model.install.trials = append(model.install.trials, trialStep{spec: spec, label: trialLabel(model.messages, spec)})
	}
	if len(model.install.trials) == 0 {
		model.install.cancel()
		return model.quitIfAsked()
	}
	return model.runTrial(0)
}

func (model *setupModel) runTrial(index int) tea.Cmd {
	model.install.trials[index].state = probeRunning
	command := model.install.trials[index].spec.command
	runner, limit, parent := model.deps.run, model.deps.trialWait, model.install.ctx
	gate := newStepGate()
	model.install.gate = gate
	return func() tea.Msg {
		if !gate.enter() {
			return trialDoneMsg{index: index, err: errInstallAborted}
		}
		defer gate.leave()
		out, err := runTrialIn(parent, runner, command, limit)
		return trialDoneMsg{index: index, output: string(out), err: err}
	}
}

func (model *setupModel) trialDone(done trialDoneMsg) tea.Cmd {
	trial := &model.install.trials[done.index]
	trial.output = lastLines(done.output, outputTail)
	trial.err = done.err
	switch {
	case trialAnswered(done.output, done.err):
		trial.state = probePassed
	case model.install.quitting:
		trial.state = probeIdle
	default:
		trial.state = probeFailed
	}
	if done.index+1 < len(model.install.trials) && !model.install.quitting {
		return model.runTrial(done.index + 1)
	}
	model.install.cancel()
	return model.quitIfAsked()
}

func (run installRun) trialsRunning() bool {
	for idx := range run.trials {
		if run.trials[idx].state == probeRunning {
			return true
		}
	}
	return false
}

func (run installRun) trialFailed() bool {
	for idx := range run.trials {
		if run.trials[idx].state == probeFailed {
			return true
		}
	}
	return false
}

func (run installRun) trialsAnswered() bool {
	for idx := range run.trials {
		if run.trials[idx].state != probePassed {
			return false
		}
	}
	return true
}
