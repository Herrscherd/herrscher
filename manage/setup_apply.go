package manage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Herrscherd/herrscher/core/clitext"
)

const (
	installTimeout = 15 * time.Minute
	outputTail     = 8
)

type installStep struct {
	run           func(context.Context) ([]byte, error)
	label         string
	output        string
	state         probeState
	writesPlugins bool
}

type installRun struct {
	err        error
	restoreErr error
	source     []byte
	rendered   []byte
	steps      []installStep
	started    bool
	written    bool
	restored   bool
	done       bool
}

func (run installRun) finished() bool { return run.done || run.err != nil }

type stepDoneMsg struct {
	err    error
	output string
	index  int
}

func (model *setupModel) startInstall() tea.Cmd {
	if model.install.started && !model.install.finished() {
		return nil
	}
	model.install = installRun{started: true}
	model.install.steps = model.installSteps()
	return model.runStep(0)
}

func (model *setupModel) installSteps() []installStep {
	written := model.draft.written()
	envPath := model.deps.envPath
	steps := []installStep{{
		label: model.text(clitext.SetupStepEnv, clitext.Params{clitext.ParamPath: envPath}),
		run: func(context.Context) ([]byte, error) {
			_, err := writeSecretsTo(envPath, written)
			return nil, err
		},
	}}
	if !model.deps.compose() {
		return steps
	}
	return append(steps, model.buildSteps()...)
}

func (model *setupModel) buildSteps() []installStep {
	source := model.deps.manifest
	model.install.source = source
	paths := model.modules.paths(model.extras)
	rendered, err := setPlugins(model.messages, string(source), paths)
	if err != nil {
		return []installStep{{label: model.text(clitext.SetupStepPlugins, nil), run: func(context.Context) ([]byte, error) { return nil, err }}}
	}
	if rendered == string(source) {
		return []installStep{{label: model.text(clitext.SetupStepUnchanged, nil), run: func(context.Context) ([]byte, error) { return nil, nil }}}
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
	for _, module := range paths {
		if module == hostModule || strings.HasPrefix(module, hostModule+"/") {
			continue
		}
		steps = append(steps, model.goStep(verbGet, "--", module))
	}
	return append(steps, model.goStep("mod", "tidy"), model.goStep("build", "./..."))
}

func (model *setupModel) goStep(args ...string) installStep {
	command := setupCommand{dir: model.deps.hostDir, name: "go", args: args}
	runner := model.deps.run
	return installStep{
		label: "go " + strings.Join(args, " "),
		run:   func(ctx context.Context) ([]byte, error) { return runner(ctx, command) },
	}
}

func (model *setupModel) runStep(index int) tea.Cmd {
	model.install.steps[index].state = probeRunning
	work := model.install.steps[index].run
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), installTimeout)
		defer cancel()
		out, err := work(ctx)
		return stepDoneMsg{index: index, output: string(out), err: err}
	}
}

func (model *setupModel) stepDone(done stepDoneMsg) tea.Cmd {
	step := &model.install.steps[done.index]
	step.output = lastLines(done.output, outputTail)
	if done.err != nil {
		step.state = probeFailed
		model.install.err = done.err
		model.restorePlugins()
		return nil
	}
	step.state = probePassed
	if step.writesPlugins {
		model.install.written = true
	}
	if done.index+1 < len(model.install.steps) {
		return model.runStep(done.index + 1)
	}
	model.installed()
	return nil
}

func (model *setupModel) restorePlugins() {
	if !model.install.written {
		return
	}
	manifest := filepath.Join(model.deps.hostDir, manifestFile)
	if err := os.WriteFile(manifest, model.install.source, modeFile); err != nil {
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
