package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/Herrscherd/herrscher/core/clitext"
)

const callTimeout = 5 * time.Minute

var (
	errBudgetStop = errors.New("membench: spend ceiling reached")
	errCanceled   = errors.New("membench: run canceled")
	errNoUsage    = errors.New("membench: the call wrote no usage record")
)

type outcome struct {
	section     clitext.Key
	group       clitext.Key
	task        string
	setting     string
	on          bool
	success     bool
	falseRecall bool
	turns       int
	tokens      int64
	input       int64
}

type execution struct {
	outcomes []outcome
	stopped  bool
}

type runner struct {
	binary      string
	model       string
	credentials string
	parent      string
	meter       *meter
}

func (r *runner) runAll(ctx context.Context, tasks []task, settings []setting) (execution, error) {
	var result execution
	err := r.forEachArm(ctx, tasks, settings, func(got outcome) {
		result.outcomes = append(result.outcomes, got)
	})
	if errors.Is(err, errBudgetStop) || errors.Is(err, errCanceled) {
		result.stopped = true
		return result, nil
	}
	return result, err
}

type arm struct {
	current task
	option  setting
	enabled bool
}

func planArms(tasks []task, settings []setting) []arm {
	var plan []arm
	for index := range tasks {
		for _, option := range settings {
			plan = append(plan,
				arm{current: tasks[index], option: option, enabled: false},
				arm{current: tasks[index], option: option, enabled: true},
			)
		}
	}
	return plan
}

func (r *runner) forEachArm(ctx context.Context, tasks []task, settings []setting, keep func(outcome)) error {
	plan := planArms(tasks, settings)
	for index := range plan {
		if !r.meter.mayStart() {
			return errBudgetStop
		}
		if ctx.Err() != nil {
			return errCanceled
		}
		got, err := r.runArm(ctx, plan[index].current, plan[index].option, plan[index].enabled)
		if err != nil {
			if ctx.Err() != nil {
				return errCanceled
			}
			return err
		}
		keep(got)
	}
	return nil
}

func (r *runner) runArm(ctx context.Context, current task, option setting, enabled bool) (outcome, error) {
	box, err := newSandbox(r.parent)
	if err != nil {
		return outcome{}, err
	}
	got, err := r.play(ctx, box, current, option, enabled)
	return got, errors.Join(err, box.close())
}

func (r *runner) play(ctx context.Context, box sandbox, current task, option setting, enabled bool) (outcome, error) {
	if err := box.installCredentials(r.credentials); err != nil {
		return outcome{}, err
	}
	assignments := []string{option.assignment(enabled)}
	requests := make([]step, 0, len(current.steps)+1)
	requests = append(requests, current.steps...)
	requests = append(requests, current.ask)
	var (
		total  tally
		answer string
	)
	for _, request := range requests {
		if !r.meter.mayStart() {
			return outcome{}, errBudgetStop
		}
		text, cumulative, err := r.call(ctx, box, current, request, assignments)
		if err != nil {
			return outcome{}, err
		}
		delta := cumulative.minus(total)
		if delta.turns == 0 {
			return outcome{}, runFailure(current.id, errNoUsage)
		}
		r.meter.charge(delta.spend())
		total = cumulative
		answer = text
	}
	success, falseRecall := score(current, answer)
	return outcome{
		section:     current.section,
		group:       current.group,
		task:        current.id,
		setting:     option.name,
		on:          enabled,
		success:     success,
		falseRecall: falseRecall,
		turns:       total.turns,
		tokens:      total.tokens,
		input:       total.input,
	}, nil
}

func (r *runner) call(ctx context.Context, box sandbox, current task, request step, assignments []string) (string, tally, error) {
	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	cmd := exec.CommandContext(callCtx, r.binary, "--print", request.prompt)
	cmd.Dir = box.home
	cmd.Env = box.environ(r.model, request.agent, assignments)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", tally{}, runFailure(current.id, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String())))
	}
	cumulative, err := readTally(box.state)
	if err != nil {
		return "", tally{}, runFailure(current.id, err)
	}
	return stdout.String(), cumulative, nil
}

func runFailure(label string, cause error) error {
	return messageError{
		key:    clitext.MembenchErrRun,
		params: clitext.Params{clitext.ParamRun: label, clitext.ParamErr: cause.Error()},
	}
}
