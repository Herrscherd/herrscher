// Command membench measures the memory settings against each other, under a spend ceiling.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/Herrscherd/herrscher-contracts/i18n"
)

type exitCode int

const (
	exitComplete exitCode = 0
	exitFailed   exitCode = 1
	exitUsage    exitCode = 2
	exitPartial  exitCode = 3
)

const (
	armsPerSetting = 2
	benchDirName   = "herrscher-membench"
)

func main() {
	os.Exit(int(run(os.Args[1:], os.Stdout, os.Stderr, os.Getenv)))
}

func run(args []string, stdout, stderr io.Writer, getenv func(string) string) exitCode {
	lang := defaultLanguage(getenv)
	msgs := loadCatalogue()
	cfg, err := parseConfig(args, stderr, msgs, lang, getenv)
	if errors.Is(err, errUsageRequested) {
		return exitComplete
	}
	if err != nil {
		return fail(stderr, msgs, lang, err, exitUsage)
	}
	tasks, err := loadTasks(cfg)
	if err != nil {
		return fail(stderr, msgs, cfg.lang, err, exitFailed)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	rep, runErr := execute(ctx, cfg, tasks, msgs)
	if writeErr := writeReport(stdout, rep, cfg, msgs); writeErr != nil {
		return fail(stderr, msgs, cfg.lang, writeErr, exitFailed)
	}
	if runErr != nil {
		return fail(stderr, msgs, cfg.lang, runErr, exitFailed)
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

func loadTasks(cfg config) ([]task, error) {
	tasks := pairTasks()
	if cfg.dataset == "" || cfg.samples == 0 {
		return tasks, nil
	}
	sampled, err := loadLongMemEval(cfg.dataset, cfg.samples, cfg.seed, cfg.maxTurns)
	if err != nil {
		return nil, err
	}
	return append(tasks, sampled...), nil
}

func execute(ctx context.Context, cfg config, tasks []task, msgs catalogue) (report, error) {
	parent, err := benchParent()
	if err != nil {
		return report{}, err
	}
	rep, runErr := measure(ctx, cfg, tasks, parent)
	cleanupErr := os.RemoveAll(parent)
	if runErr != nil {
		rep.Status = statusFailed
		rep.Failure = failureText(msgs, cfg.lang, runErr)
		return rep, errors.Join(runErr, cleanupErr)
	}
	return rep, cleanupErr
}

func benchParent() (string, error) {
	cacheRoot, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("membench cache: %w", err)
	}
	benchRoot := filepath.Join(cacheRoot, benchDirName)
	if err = os.MkdirAll(benchRoot, dirMode); err != nil {
		return "", fmt.Errorf("membench cache: %w", err)
	}
	parent, err := os.MkdirTemp(benchRoot, "run-")
	if err != nil {
		return "", fmt.Errorf("membench cache: %w", err)
	}
	return parent, nil
}

func measure(ctx context.Context, cfg config, tasks []task, parent string) (report, error) {
	ceiling := &meter{limit: cfg.budget}
	bench := runner{
		binary:      cfg.binary,
		model:       cfg.model,
		credentials: cfg.credentials,
		apiKey:      cfg.apiKey,
		parent:      parent,
		meter:       ceiling,
	}
	executed, err := bench.runAll(ctx, tasks, cfg.settings)
	planned := len(tasks) * len(cfg.settings) * armsPerSetting
	return summarize(cfg, executed, ceiling.spent, planned), err
}

func fail(stderr io.Writer, msgs catalogue, lang i18n.Language, err error, code exitCode) exitCode {
	if _, writeErr := fmt.Fprintln(stderr, failureText(msgs, lang, err)); writeErr != nil {
		return exitFailed
	}
	return code
}

func failureText(msgs catalogue, lang i18n.Language, err error) string {
	known, ok := errors.AsType[messageError](err)
	if !ok {
		return err.Error()
	}
	translator := msgs.For(lang)
	text := translator.Text(known.key, known.params)
	if translator.Err() != nil {
		return err.Error()
	}
	return text
}
