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
	exitComplete    exitCode = 0
	exitFailed      exitCode = 1
	exitUsage       exitCode = 2
	exitPartial     exitCode = 3
	exitInterrupted exitCode = 4
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
	tasks, skipped, err := loadTasks(cfg)
	if err != nil {
		return fail(stderr, msgs, cfg.lang, err, exitFailed)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	rep, runErr := execute(ctx, cfg, tasks, skipped, msgs)
	if writeErr := writeReport(stdout, rep, cfg, msgs); writeErr != nil {
		return fail(stderr, msgs, cfg.lang, writeErr, exitFailed)
	}
	if runErr != nil {
		return fail(stderr, msgs, cfg.lang, runErr, exitFailed)
	}
	switch rep.Status {
	case statusInterrupted:
		return exitInterrupted
	case statusPartial:
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

func loadTasks(cfg config) ([]task, int, error) {
	tasks := pairTasks()
	if cfg.dataset == "" || cfg.samples == 0 {
		return tasks, 0, nil
	}
	sampled, skipped, err := loadLongMemEval(cfg.dataset, cfg.samples, cfg.seed, cfg.maxTurns)
	if err != nil {
		return nil, 0, err
	}
	return append(tasks, sampled...), skipped, nil
}

func plannedArms(cfg config, tasks []task) int {
	return len(tasks) * len(cfg.settings) * armsPerSetting
}

func execute(ctx context.Context, cfg config, tasks []task, skipped int, msgs catalogue) (report, error) {
	parent, err := benchParent()
	if err != nil {
		return failedReport(reportFrame(cfg, plannedArms(cfg, tasks)), msgs, cfg.lang, err)
	}
	rep, runErr := measure(ctx, cfg, tasks, skipped, parent)
	cleanupErr := os.RemoveAll(parent)
	if failure := errors.Join(runErr, cleanupErr); failure != nil {
		return failedReport(rep, msgs, cfg.lang, failure)
	}
	return rep, nil
}

func failedReport(rep report, msgs catalogue, lang i18n.Language, cause error) (report, error) {
	rep.Status = statusFailed
	rep.Failure = failureText(msgs, lang, cause)
	return rep, cause
}

func benchParent() (string, error) {
	cacheRoot, err := os.UserCacheDir()
	if err != nil {
		return "", cacheError(err)
	}
	benchRoot := filepath.Join(cacheRoot, benchDirName)
	if err = os.MkdirAll(benchRoot, dirMode); err != nil {
		return "", pathError(msgErrCacheRoot, benchRoot, err)
	}
	if sweepErr := sweepOrphans(benchRoot); sweepErr != nil {
		return "", sweepErr
	}
	parent, err := os.MkdirTemp(benchRoot, "run-")
	if err != nil {
		return "", sandboxError(err)
	}
	if err = writeOwner(parent); err != nil {
		return "", errors.Join(err, os.RemoveAll(parent))
	}
	return parent, nil
}

func measure(ctx context.Context, cfg config, tasks []task, skipped int, parent string) (report, error) {
	ceiling := &meter{limit: cfg.budget, reserve: cfg.reserve}
	bench := runner{
		binary:      cfg.binary,
		model:       cfg.model,
		credentials: cfg.credentials,
		apiKey:      cfg.apiKey,
		parent:      parent,
		meter:       ceiling,
	}
	executed, err := bench.runAll(ctx, tasks, cfg.settings)
	rep := summarize(cfg, executed, ceiling.spent, plannedArms(cfg, tasks))
	rep.SkippedLong = skipped
	return rep, err
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
		known = messageError{key: msgErrSystem}
	}
	translator := msgs.For(lang)
	text := translator.Text(known.key, known.params)
	if translator.Err() != nil {
		return string(msgErrSystem)
	}
	return text
}
