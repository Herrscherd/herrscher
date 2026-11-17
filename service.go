package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/service"
)

var (
	errServiceUsage       = errors.New("service usage")
	errUnknownSubcommand  = errors.New("unknown subcommand")
	errUnexpectedArgument = errors.New("unexpected argument")
)

func runService(ctx context.Context, messages *clitext.Messages, args []string) error {
	lang := clitext.ClientLanguage()
	if len(args) == 0 {
		return serviceUsageError(messages, lang)
	}
	sub := args[0]
	switch sub {
	case cmdInstall, "uninstall", cmdStatus, "restart", cmdUpdate:
	default:
		return unknownSubcommand(messages, lang, sub)
	}

	cfg, err := service.DefaultConfig(messages)
	if err != nil {
		return err
	}

	switch sub {
	case "restart":
		if err := service.Restart(ctx, messages, cfg); err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, render(messages, lang, clitext.ServiceRestarted, nil))
		return nil
	case cmdUpdate:
		return runServiceUpdate(ctx, messages, cfg, args[1:])
	}

	translator := messages.For(lang)
	flags := flag.NewFlagSet("service", flag.ContinueOnError)
	healthAddr := flags.String("health-addr", cfg.HealthAddr, translator.Text(clitext.ServiceFlagHealthAddr, nil))
	envFile := flags.String("env-file", cfg.EnvFile, translator.Text(clitext.ServiceFlagEnvFile, nil))
	defaultCmd := flags.String("cmd", "", translator.Text(clitext.ServiceFlagCmd, nil))
	if err := translator.Err(); err != nil {
		return err
	}
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return unexpectedArgument(messages, lang, sub, flags.Arg(0))
	}
	cfg.HealthAddr = *healthAddr
	cfg.EnvFile = *envFile

	switch sub {
	case cmdInstall:
		cfg.DefaultCmd = *defaultCmd
		return service.Install(ctx, messages, cfg)
	case "uninstall":
		if err := service.Uninstall(ctx, messages, cfg); err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, render(messages, lang, clitext.ServiceRemoved, nil))
		return nil
	case cmdStatus:
		return service.Status(ctx, messages, cfg)
	default:
		return unknownSubcommand(messages, lang, sub)
	}
}

func serviceUsageError(messages *clitext.Messages, lang i18n.Language) error {
	return messages.Failure(lang, errServiceUsage, clitext.ServiceUsage, nil)
}

func unknownSubcommand(messages *clitext.Messages, lang i18n.Language, sub string) error {
	return messages.Failure(lang, errUnknownSubcommand, clitext.ServiceUnknownSubcommand, clitext.Params{clitext.ParamName: strconv.Quote(sub)})
}

func unexpectedArgument(messages *clitext.Messages, lang i18n.Language, sub, arg string) error {
	return messages.Failure(lang, errUnexpectedArgument, clitext.ServiceUnexpectedArgument, clitext.Params{
		clitext.ParamVerb:  sub,
		clitext.ParamArg:   strconv.Quote(arg),
		clitext.ParamUsage: render(messages, lang, clitext.ServiceUsage, nil),
	})
}

func runServiceUpdate(ctx context.Context, messages *clitext.Messages, cfg service.Config, args []string) error {
	lang := clitext.ClientLanguage()
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	translator := messages.For(lang)
	flags := flag.NewFlagSet("service update", flag.ContinueOnError)
	source := flags.String("source", cwd, translator.Text(clitext.ServiceFlagSource, nil))
	noPull := flags.Bool("no-pull", false, translator.Text(clitext.ServiceFlagNoPull, nil))
	if err := translator.Err(); err != nil {
		return err
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return messages.Failure(lang, errUnexpectedArgument, clitext.ServiceUpdateUnexpectedArg, clitext.Params{clitext.ParamArg: strconv.Quote(flags.Arg(0))})
	}
	if installed, ok := service.InstalledBinPath(cfg); ok && installed != cfg.BinPath {
		fmt.Fprintln(os.Stderr, render(messages, lang, clitext.ServiceUpdateRebuilding, clitext.Params{clitext.ParamInstalled: installed, clitext.ParamRan: cfg.BinPath}))
		cfg.BinPath = installed
	}
	if err := service.Update(ctx, messages, cfg, *source, !*noPull); err != nil {
		return err
	}
	v := service.SourceVersion(ctx, *source)
	fmt.Fprintln(os.Stderr, render(messages, lang, clitext.ServiceUpdateRebuilt, clitext.Params{clitext.ParamVersion: v}))
	return nil
}
