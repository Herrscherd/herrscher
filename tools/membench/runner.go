package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	callTimeout            = 5 * time.Minute
	anthropicEnvName       = "ANTHROPIC_API_KEY"
	redactedSecret         = "[redacted]"
	maxPromptBytes         = 128<<10 - 1
	consolidateEveryRunEnv = "TERMINAL_CONSOLIDATE_EVERY=0"
)

var (
	errBudgetStop = errors.New("membench: spend ceiling reached")
	errCanceled   = errors.New("membench: run canceled")
)

type outcome struct {
	section     msgKey
	group       msgKey
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
	apiKey      string
	parent      string
	meter       *meter
}

func (r *runner) runAll(ctx context.Context, tasks []task, settings []setting) (execution, error) {
	var result execution
	err := r.forEachArm(ctx, tasks, settings, func(got outcome) {
		result.outcomes = append(result.outcomes, got)
	})
	result.stopped = err != nil
	if errors.Is(err, errBudgetStop) || errors.Is(err, errCanceled) {
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
		got, err := r.runArm(ctx, plan[index])
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

func (r *runner) runArm(ctx context.Context, planned arm) (outcome, error) {
	box, err := newSandbox(r.parent)
	if err != nil {
		return outcome{}, err
	}
	got, err := r.play(ctx, box, planned)
	return got, errors.Join(err, box.close())
}

func (r *runner) play(ctx context.Context, box sandbox, planned arm) (outcome, error) {
	if err := box.installCredentials(r.credentials); err != nil {
		return outcome{}, err
	}
	if err := box.plantAgedNode(); err != nil {
		return outcome{}, err
	}
	assignments := r.assignments(box, planned.option, planned.enabled)
	requests := append(append([]step{}, planned.current.steps...), planned.current.ask)
	var (
		total  tally
		answer string
	)
	for _, request := range requests {
		if !r.meter.mayStart() {
			return outcome{}, errBudgetStop
		}
		text, cumulative, err := r.call(ctx, box, planned.current, request, assignments)
		if err != nil {
			r.settle(&total, cumulative)
			return outcome{}, err
		}
		delta := cumulative.minus(total)
		if delta.turns == 0 {
			return outcome{}, messageError{key: msgErrNoUsage, params: params{paramRun: planned.current.id}}
		}
		if r.meter.limit.usd > 0 && delta.usd <= 0 {
			return outcome{}, messageError{key: msgErrNoCost, params: params{paramRun: planned.current.id}}
		}
		r.settle(&total, cumulative)
		answer = text
	}
	success, falseRecall := score(planned.current, answer)
	return outcome{
		section:     planned.current.section,
		group:       planned.current.group,
		task:        planned.current.id,
		setting:     planned.option.name,
		on:          planned.enabled,
		success:     success,
		falseRecall: falseRecall,
		turns:       total.turns,
		tokens:      total.tokens,
		input:       total.input,
	}, nil
}

func (r *runner) settle(total *tally, cumulative tally) {
	delta := cumulative.minus(*total)
	if delta.turns <= 0 {
		return
	}
	r.meter.charge(delta.spend())
	*total = cumulative
}

func (r *runner) assignments(box sandbox, option setting, enabled bool) []string {
	list := []string{option.assignment(enabled), consolidateEveryRunEnv, vaultEnv + "=" + box.vault()}
	if r.apiKey != "" {
		list = append(list, anthropicEnvName+"="+r.apiKey)
	}
	return list
}

func (r *runner) call(ctx context.Context, box sandbox, current task, request step, assignments []string) (string, tally, error) {
	if len(request.prompt) > maxPromptBytes {
		return "", tally{}, messageError{key: msgErrPromptLong, params: params{paramRun: current.id, paramLimit: strconv.Itoa(maxPromptBytes)}}
	}
	text, invokeErr := r.invoke(ctx, box, request, assignments)
	cumulative, readErr := readTally(box.state)
	if readErr != nil {
		return "", tally{}, runFailure(current.id, errors.Join(invokeErr, readErr))
	}
	if invokeErr != nil {
		return "", cumulative, runFailure(current.id, invokeErr)
	}
	return text, cumulative, nil
}

func (r *runner) invoke(ctx context.Context, box sandbox, request step, assignments []string) (string, error) {
	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	cmd := exec.CommandContext(callCtx, r.binary, "--print", request.prompt)
	cmd.Dir = box.home
	cmd.Env = box.environ(r.model, request.agent, assignments)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s", err, redact(strings.TrimSpace(stderr.String()), r.apiKey))
	}
	return stdout.String(), nil
}

func redact(text, secret string) string {
	if secret == "" {
		return text
	}
	return strings.ReplaceAll(text, secret, redactedSecret)
}

func runFailure(label string, cause error) error {
	if errors.Is(cause, context.DeadlineExceeded) {
		return messageError{key: msgErrRunTimeout, params: params{paramRun: label}}
	}
	if exit, ok := errors.AsType[*exec.ExitError](cause); ok {
		return messageError{key: msgErrRunExit, params: params{paramRun: label, paramCode: strconv.Itoa(exit.ExitCode())}}
	}
	return messageError{key: msgErrRun, params: params{paramRun: label}}
}
