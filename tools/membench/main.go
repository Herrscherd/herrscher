// Command membench runs the memory evaluation bench: the same tasks with and without
// each memory setting, each run in its own isolated state directory, under a hard
// spend ceiling. It is run on demand and never from CI or go test ./..., and it
// reaches a real model only when it is started with a budget.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
)

type exitCode int

const (
	exitComplete exitCode = 0
	exitFailed   exitCode = 1
	exitUsage    exitCode = 2
	exitPartial  exitCode = 3
)

const armsPerSetting = 2

func main() {
	os.Exit(int(run(os.Args[1:], os.Stdout, os.Stderr, os.Getenv)))
}

func run(args []string, stdout, stderr io.Writer, getenv func(string) string) exitCode {
	lang := defaultLanguage(getenv)
	msgs := clitext.NewMessages()
	cfg, err := parseConfig(args, stderr, msgs, lang)
	if errors.Is(err, errUsageRequested) {
		return exitComplete
	}
	if err != nil {
		return fail(stderr, msgs, lang, err, exitUsage)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	rep, err := execute(ctx, cfg)
	if err != nil {
		return fail(stderr, msgs, cfg.lang, err, exitFailed)
	}
	if err = writeReport(stdout, rep, cfg); err != nil {
		return fail(stderr, msgs, cfg.lang, err, exitFailed)
	}
	if rep.Status == statusPartial {
		return exitPartial
	}
	return exitComplete
}

func defaultLanguage(getenv func(string) string) i18n.Language {
	lang, ok := i18n.FromEnv(getenv)
	if !ok {
		return i18n.English
	}
	return lang
}

func execute(ctx context.Context, cfg config) (report, error) {
	tasks := pairTasks()
	if cfg.dataset != "" && cfg.samples > 0 {
		sampled, err := loadLongMemEval(cfg.dataset, cfg.samples, cfg.seed, cfg.maxTurns)
		if err != nil {
			return report{}, err
		}
		tasks = append(tasks, sampled...)
	}
	parent, err := os.MkdirTemp("", "membench-")
	if err != nil {
		return report{}, fmt.Errorf("membench: %w", err)
	}
	rep, runErr := measure(ctx, cfg, tasks, parent)
	return rep, errors.Join(runErr, os.Remove(parent))
}

func measure(ctx context.Context, cfg config, tasks []task, parent string) (report, error) {
	ceiling := &meter{limit: cfg.budget}
	bench := runner{
		binary:      cfg.binary,
		model:       cfg.model,
		credentials: cfg.credentials,
		parent:      parent,
		meter:       ceiling,
	}
	executed, err := bench.runAll(ctx, tasks, cfg.settings)
	if err != nil {
		return report{}, err
	}
	planned := len(tasks) * len(cfg.settings) * armsPerSetting
	return summarize(cfg, executed, ceiling.spent, planned), nil
}

func fail(stderr io.Writer, msgs *clitext.Messages, lang i18n.Language, err error, code exitCode) exitCode {
	if _, writeErr := fmt.Fprintln(stderr, failureText(msgs, lang, err)); writeErr != nil {
		return exitFailed
	}
	return code
}

func failureText(msgs *clitext.Messages, lang i18n.Language, err error) string {
	if known, ok := errors.AsType[messageError](err); ok {
		translator := msgs.For(lang)
		text := translator.Text(known.key, known.params)
		if translator.Err() == nil {
			return text
		}
	}
	return err.Error()
}
