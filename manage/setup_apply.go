package manage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Herrscherd/herrscher/core/clitext"
)

const (
	installTimeout = 15 * time.Minute
	outputTail     = 8
	abandonLimit   = 30 * time.Second
)

var (
	errInstallAborted = clitext.SentinelError("install aborted")
	errStepStillRuns  = clitext.SentinelError("install step still running")
)

type installStep struct {
	run           func(context.Context) ([]byte, error)
	label         string
	output        string
	state         probeState
	writesPlugins bool
}

type installRun struct {
	ctx        context.Context
	err        error
	restoreErr error
	cancel     context.CancelFunc
	gate       *stepGate
	saved      map[string][]byte
	rendered   []byte
	steps      []installStep
	trials     []trialStep
	started    bool
	touched    bool
	restored   bool
	done       bool
	quitting   bool
}

func (run installRun) finished() bool { return run.done || run.err != nil }

func (run installRun) running() bool { return run.started && !run.finished() }

type stepGate struct {
	idle    chan struct{}
	done    sync.Once
	mu      sync.Mutex
	entered bool
	shut    bool
}

func newStepGate() *stepGate { return &stepGate{idle: make(chan struct{})} }

func (gate *stepGate) enter() bool {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	gate.entered = !gate.shut
	return gate.entered
}

func (gate *stepGate) leave() { gate.done.Do(func() { close(gate.idle) }) }

func (gate *stepGate) close() <-chan struct{} {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	gate.shut = true
	if gate.entered {
		return gate.idle
	}
	return nil
}

type stepDoneMsg struct {
	err    error
	output string
	index  int
}

func (model *setupModel) startInstall() tea.Cmd {
	if model.install.running() || model.install.trialsRunning() {
		return nil
	}
	ctx, cancel := context.WithCancel(model.ctx)
	model.install = installRun{started: true, ctx: ctx, cancel: cancel}
	model.install.steps = model.installSteps()
	return model.runStep(0)
}

func (model *setupModel) installSteps() []installStep {
	written := model.draft.written()
	envPath := model.deps.envPath
	steps := []installStep{{
		label: model.text(clitext.SetupStepEnv, clitext.Params{clitext.ParamPath: envPath}),
		run: func(context.Context) ([]byte, error) {
			return nil, writeSecretsTo(envPath, written)
		},
	}}
	if !model.deps.compose() {
		return steps
	}
	return append(steps, model.buildSteps()...)
}

func (model *setupModel) buildSteps() []installStep {
	source := model.deps.manifest
	paths := model.modules.paths(model.extras)
	rendered, err := setPlugins(model.messages, string(source), paths)
	if err == nil && rendered == string(source) {
		return []installStep{{label: model.text(clitext.SetupStepUnchanged, nil), run: func(context.Context) ([]byte, error) { return nil, nil }}}
	}
	if err == nil {
		model.install.saved, err = saveComposition(model.messages, model.deps.hostDir)
	}
	if err != nil {
		return []installStep{{label: model.text(clitext.SetupStepPlugins, nil), run: func(context.Context) ([]byte, error) { return nil, err }}}
	}
	model.install.rendered = []byte(rendered)
	manifest := filepath.Join(model.deps.hostDir, manifestFile)
	steps := []installStep{{
		label:         model.text(clitext.SetupStepPlugins, nil),
		writesPlugins: true,
		run: func(context.Context) ([]byte, error) {
			return nil, os.WriteFile(manifest, []byte(rendered), modeFile)
		},
	}}
	runner := model.deps.run
	target := model.deps.binary
	for _, command := range stackCommands(model.deps.hostDir, target, paths) {
		steps = append(steps, installStep{
			label: command.name + " " + strings.Join(command.args, " "),
			run:   func(ctx context.Context) ([]byte, error) { return runner(ctx, command) },
		})
	}
	return append(steps, installStep{
		label: model.text(clitext.SetupStepBinary, clitext.Params{clitext.ParamPath: target}),
		run:   func(context.Context) ([]byte, error) { return nil, swapBinary(target) },
	})
}

func (model *setupModel) runStep(index int) tea.Cmd {
	model.install.steps[index].state = probeRunning
	model.install.touched = model.install.touched || model.install.steps[index].writesPlugins
	work := model.install.steps[index].run
	parent := model.install.ctx
	gate := newStepGate()
	model.install.gate = gate
	return func() tea.Msg {
		if !gate.enter() {
			return stepDoneMsg{index: index, err: errInstallAborted}
		}
		defer gate.leave()
		ctx, cancel := context.WithTimeout(parent, installTimeout)
		defer cancel()
		out, err := work(ctx)
		return stepDoneMsg{index: index, output: string(out), err: err}
	}
}

func (model *setupModel) stepDone(done stepDoneMsg) tea.Cmd {
	step := &model.install.steps[done.index]
	step.output = lastLines(done.output, outputTail)
	switch {
	case done.err != nil:
		step.state = probeFailed
		model.install.err = done.err
	case done.index+1 == len(model.install.steps):
		step.state = probePassed
		model.installed()
		if model.install.quitting {
			model.install.cancel()
			return tea.Quit
		}
		return model.startTrials()
	default:
		step.state = probePassed
		if !model.install.quitting {
			return model.runStep(done.index + 1)
		}
		model.install.err = errInstallAborted
	}
	model.install.cancel()
	model.restorePlugins()
	return model.quitIfAsked()
}

func (model *setupModel) quitIfAsked() tea.Cmd {
	if model.install.quitting {
		return tea.Quit
	}
	return nil
}

func (model *setupModel) abortInstall() {
	model.install.quitting = true
	model.install.cancel()
}

func (model *setupModel) abandon() {
	if model.install.trialsRunning() {
		model.install.cancel()
	}
	if !model.install.running() {
		return
	}
	model.install.cancel()
	model.install.err = errInstallAborted
	if !model.waitStep() {
		model.install.restoreErr = failure(model.messages, errStepStillRuns, clitext.SetupInstallNotRestored, nil)
		return
	}
	model.restorePlugins()
}

func (model *setupModel) waitStep() bool {
	busy := model.install.gate.close()
	if busy == nil {
		return true
	}
	select {
	case <-busy:
		return true
	case <-time.After(model.deps.abandonWait):
		return false
	}
}

func (model *setupModel) restorePlugins() {
	if !model.install.touched {
		return
	}
	if err := restoreComposition(model.messages, model.deps.hostDir, model.install.saved); err != nil {
		model.install.restoreErr = err
		return
	}
	model.install.restored = true
}

func (model *setupModel) installed() {
	model.install.done = true
	model.envExists = true
	for key, value := range model.draft.written() {
		model.draft.initial[key] = value
		delete(model.draft.edits, key)
	}
	if model.install.rendered != nil {
		model.deps.manifest = model.install.rendered
	}
}

func lastLines(output string, limit int) string {
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return strings.Join(lines, "\n")
}
