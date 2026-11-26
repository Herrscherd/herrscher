package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/host"
)

const (
	hostVerbReadWait  = 2 * time.Minute
	hostVerbBuildWait = 15 * time.Minute
	paramModule       = "module"
	paramProbe        = "probe"
	paramNoBuild      = "no_build"
	familySetup       = "setup"
	familyConnect     = "connect"
	familyPlugin      = "plugin"
	verbCheck         = "check"
	verbStatus        = "status"
	verbList          = "list"
	verbAdd           = "add"
	verbRm            = "rm"
	verbRemove        = "remove"
	flagYes           = "--yes"
	flagNoBuild       = "--no-build"
)

var (
	errHostVerbBinary = errors.New("herrscher: this binary cannot name itself, so the daemon cannot run the verb")
	errModuleFlag     = errors.New("herrscher plugin: a module path never starts with -")
)

type hostVerbError struct {
	verb   string
	output string
	cause  error
}

func (f *hostVerbError) Error() string {
	text := strings.TrimSpace(f.output)
	if text == "" {
		return "herrscher " + f.verb + ": " + f.cause.Error()
	}
	return text
}

func (f *hostVerbError) Unwrap() error { return f.cause }

type selfExec struct {
	path          []string
	help          string
	wait          time.Duration
	argv          func(contracts.Input) ([]string, error)
	exitIsVerdict bool
}

func (verb selfExec) declare(params ...contracts.Param) contracts.Cmd {
	builder := contracts.New(verb.path...).Help(verb.help)
	for _, declared := range params {
		if declared.ValueRequired {
			builder = builder.ValueParam(declared.Name, declared.Help, declared.Required)
			continue
		}
		builder = builder.Param(declared.Name, declared.Help, declared.Required)
	}
	return builder.Do(verb.run)
}

func (verb selfExec) run(ctx context.Context, input contracts.Input) (contracts.Result, error) {
	binary, err := os.Executable()
	if err != nil {
		return contracts.Result{}, errHostVerbBinary
	}
	bounded, cancel := context.WithTimeout(ctx, verb.wait)
	defer cancel()
	args, err := verb.argv(input)
	if err != nil {
		return contracts.Result{}, err
	}
	command := exec.CommandContext(bounded, binary, args...)
	command.Env = withoutSessionSocket(os.Environ())
	command.Stdin = bytes.NewReader(nil)
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	if runErr := command.Run(); runErr != nil {
		if _, exited := errors.AsType[*exec.ExitError](runErr); exited && verb.exitIsVerdict && strings.TrimSpace(output.String()) != "" {
			return contracts.DataResult(strings.TrimRight(output.String(), "\n")), nil
		}
		return contracts.Result{}, &hostVerbError{verb: strings.Join(verb.path, " "), output: output.String(), cause: runErr}
	}
	return contracts.DataResult(strings.TrimRight(output.String(), "\n")), nil
}

func withoutSessionSocket(environ []string) []string {
	kept := make([]string, 0, len(environ))
	for _, pair := range environ {
		if !strings.HasPrefix(pair, host.SessionCommandSocketVar+"=") {
			kept = append(kept, pair)
		}
	}
	return kept
}

func flagIf(input contracts.Input, name, flag string) []string {
	if input.Bool(name) {
		return []string{flag}
	}
	return nil
}

func jsonIf(input contracts.Input) []string {
	if input.JSON {
		return []string{"--json"}
	}
	return nil
}

func moduleOf(input contracts.Input) ([]string, error) {
	modules := input.Rest
	if module := strings.TrimSpace(input.Get(paramModule)); module != "" {
		modules = []string{module}
	}
	for _, module := range modules {
		if strings.HasPrefix(module, "-") {
			return nil, errModuleFlag
		}
	}
	return modules, nil
}

func pluginArgv(input contracts.Input, sub string) ([]string, error) {
	modules, err := moduleOf(input)
	if err != nil {
		return nil, err
	}
	return append(append([]string{familyPlugin, sub, flagYes}, flagIf(input, paramNoBuild, flagNoBuild)...), modules...), nil
}

func hostVerbs() []contracts.Cmd {
	return []contracts.Cmd{
		selfExec{path: []string{familySetup, verbCheck}, help: clitext.CmdHelpSetupCheck, wait: hostVerbReadWait, exitIsVerdict: true, argv: func(input contracts.Input) ([]string, error) {
			return append(append([]string{familySetup, verbCheck}, flagIf(input, paramProbe, "--probe")...), jsonIf(input)...), nil
		}}.declare(contracts.Param{Name: paramProbe, Help: clitext.ManageFlagSetupProbe}),
		selfExec{path: []string{familyConnect, verbStatus}, help: clitext.CmdHelpConnectStatus, wait: hostVerbReadWait, argv: func(contracts.Input) ([]string, error) {
			return []string{familyConnect, verbStatus}, nil
		}}.declare(),
		selfExec{path: []string{familyPlugin, verbList}, help: clitext.CmdHelpPluginList, wait: hostVerbReadWait, argv: func(contracts.Input) ([]string, error) {
			return []string{familyPlugin, verbList}, nil
		}}.declare(),
		selfExec{path: []string{familyPlugin, verbAdd}, help: clitext.CmdHelpPluginAdd, wait: hostVerbBuildWait, argv: func(input contracts.Input) ([]string, error) {
			return pluginArgv(input, verbAdd)
		}}.declare(contracts.Param{Name: paramModule, Help: clitext.CmdParamPluginModule, ValueRequired: true}, contracts.Param{Name: paramNoBuild, Help: clitext.CmdParamPluginNoBuild}),
		selfExec{path: []string{familyPlugin, verbRm}, help: clitext.CmdHelpPluginRemove, wait: hostVerbBuildWait, argv: func(input contracts.Input) ([]string, error) {
			return pluginArgv(input, verbRemove)
		}}.declare(contracts.Param{Name: paramModule, Help: clitext.CmdParamPluginModule, ValueRequired: true}, contracts.Param{Name: paramNoBuild, Help: clitext.CmdParamPluginNoBuild}),
	}
}
