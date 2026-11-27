package host

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/config"
	"github.com/Herrscherd/herrscher/core/internal/atomicfile"
)

const (
	nsConfig   = "config"
	paramKey   = "key"
	paramValue = "value"
)

type editLock struct {
	mu    sync.Mutex
	paths map[string]*sync.Mutex
}

func newEditLock() *editLock {
	return &editLock{paths: make(map[string]*sync.Mutex)}
}

func (l *editLock) hold(path string) func() {
	l.mu.Lock()
	lock, ok := l.paths[path]
	if !ok {
		lock = &sync.Mutex{}
		l.paths[path] = lock
	}
	l.mu.Unlock()
	lock.Lock()
	return lock.Unlock
}

func configPathOr(path string) string {
	if path != "" {
		return path
	}
	return config.DefaultPath()
}

func loadedConfig(path string) config.Config {
	configured, err := config.Load(configPathOr(path))
	if err != nil {
		slog.Warn("config.json unreadable, the self tools keep their defaults", "err", err)
	}
	return configured
}

func fileVerbs(cliMessages *clitext.Messages, options Options, notices *noticeSlot) []contracts.Cmd {
	configPath := func() string { return configPathOr(options.ConfigPath) }
	edits := newEditLock()
	return append(configVerbs(cliMessages, options.live.Get, configPath, edits), envVerbs(cliMessages, options.live.Get, daemonEnvFile, edits, notices)...)
}

func configVerbs(cliMessages *clitext.Messages, lang languageSource, path func() string, edits *editLock) []contracts.Cmd {
	return []contracts.Cmd{
		contracts.New(nsConfig, "get").
			Help(clitext.CmdHelpConfigGet).
			ValueParam(paramKey, clitext.CmdParamConfigGetKey, false).
			Do(func(_ context.Context, input contracts.Input) (contracts.Result, error) {
				doc, err := config.ReadDocument(path())
				if err != nil {
					return contracts.Result{}, err
				}
				return cli.Data(doc.Get(firstOf(input.Get(paramKey), input.Rest)))
			}),
		contracts.New(nsConfig, "set").
			Help(clitext.CmdHelpConfigSet).
			Param(paramKey, clitext.CmdParamConfigSetKey, true).
			Param(paramValue, clitext.CmdParamConfigSetValue, true).
			Do(func(_ context.Context, input contracts.Input) (contracts.Result, error) {
				return cli.Data(editConfig(cliMessages, lang, path(), input.Get(paramKey), clitext.HostConfigSet, edits, func(doc *config.Document, key string) error {
					return doc.Set(key, input.Get(paramValue))
				}))
			}),
		contracts.New(nsConfig, "unset").
			Help(clitext.CmdHelpConfigUnset).
			Param(paramKey, clitext.CmdParamConfigUnsetKey, true).
			Do(func(_ context.Context, input contracts.Input) (contracts.Result, error) {
				return cli.Data(editConfig(cliMessages, lang, path(), input.Get(paramKey), clitext.HostConfigUnset, edits, (*config.Document).Unset))
			}),
	}
}

func editConfig(cliMessages *clitext.Messages, lang languageSource, path, rawKey string, done clitext.Key, edits *editLock, edit func(*config.Document, string) error) (string, error) {
	key := strings.TrimSpace(rawKey)
	release := edits.hold(path)
	defer release()
	logCleanup(atomicfile.Sweep(path))
	doc, err := config.ReadDocument(path)
	if err != nil {
		return "", err
	}
	if editErr := edit(&doc, key); editErr != nil {
		return "", editErr
	}
	backup, err := doc.Write(path)
	if err != nil {
		return "", err
	}
	text := replyText(cliMessages, lang())
	out := text.Text(done, clitext.Params{clitext.ParamKey: key, clitext.ParamPath: path})
	if backup != "" {
		out += "\n" + text.Text(clitext.HostConfigBackup, clitext.Params{clitext.ParamPath: backup})
	}
	return out, nil
}
