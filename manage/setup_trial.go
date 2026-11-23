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

func (model *setupModel) trialSpecs() []trialSpec {
	specs := []trialSpec{
		{module: moduleClaude, section: kindClaude, command: setupCommand{name: kindClaude, args: []string{"-p", trialPrompt}, env: presentEnv(model.draft, envAnthropicKey)}},
		{module: moduleCodex, section: kindCodex, command: setupCommand{name: kindCodex, args: []string{"exec", "--skip-git-repo-check", trialPrompt}}},
		{module: moduleCursor, section: kindCursor, command: setupCommand{name: cursorCLI, args: []string{"-p", trialPrompt}, env: presentEnv(model.draft, envCursorKey)}},
	}
	chosen := make([]trialSpec, 0, len(specs))
	for _, spec := range specs {
		if model.modules[spec.module] {
			chosen = append(chosen, spec)
		}
	}
	return chosen
}

func presentEnv(draft settingsDraft, key string) []string {
	if value := draft.value(key); value != "" {
		return []string{key + "=" + value}
	}
	return nil
}

func (model *setupModel) startTrials() tea.Cmd {
	model.install.trials = nil
	for _, spec := range model.trialSpecs() {
		model.install.trials = append(model.install.trials, trialStep{spec: spec, label: model.text(clitext.SetupTrialStep, clitext.Params{clitext.ParamName: spec.command.name})})
	}
	if len(model.install.trials) == 0 {
		model.install.cancel()
		return model.quitIfAsked()
	}
	return model.runTrial(0)
}

type trialStep struct {
	spec   trialSpec
	label  string
	output string
	err    error
	state  probeState
}

func (model *setupModel) runTrial(index int) tea.Cmd {
	model.install.trials[index].state = probeRunning
	command := model.install.trials[index].spec.command
	runner := model.deps.run
	parent := model.install.ctx
	return func() tea.Msg {
		out, err := runTrialIn(parent, runner, command)
		return trialDoneMsg{index: index, output: string(out), err: err}
	}
}

func runTrialIn(parent context.Context, runner commandRunner, command setupCommand) ([]byte, error) {
	dir, err := os.MkdirTemp("", trialDirName)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(parent, trialTimeout)
	defer cancel()
	command.dir = dir
	out, err := runner(ctx, command)
	return out, errors.Join(err, os.RemoveAll(dir))
}

func (model *setupModel) trialDone(done trialDoneMsg) tea.Cmd {
	trial := &model.install.trials[done.index]
	trial.output = lastLines(done.output, outputTail)
	trial.err = done.err
	trial.state = probePassed
	if done.err != nil || strings.TrimSpace(done.output) == "" {
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

func (model *setupModel) trialRemedy(trial *trialStep) string {
	if errors.Is(trial.err, exec.ErrNotFound) {
		return model.text(clitext.SetupTrialMissing, clitext.Params{clitext.ParamName: trial.spec.command.name})
	}
	return model.text(clitext.SetupTrialRemedy, clitext.Params{clitext.ParamCommand: commandName(invokedBinary(os.Args)) + " setup " + trial.spec.section})
}
