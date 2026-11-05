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

func runService(ctx context.Context, args []string) error {
	lang := clitext.ClientLanguage()
	if len(args) == 0 {
		return serviceUsageError(lang)
	}
	sub := args[0]
	switch sub {
	case cmdInstall, "uninstall", cmdStatus, "restart", cmdUpdate:
	default:
		return unknownSubcommand(lang, sub)
	}

	cfg, err := service.DefaultConfig()
	if err != nil {
		return err
	}

	switch sub {
	case "restart":
		if err := service.Restart(ctx, cfg); err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, render(lang, clitext.ServiceRestarted, nil))
		return nil
	case cmdUpdate:
		return runServiceUpdate(ctx, cfg, args[1:])
	}

	translator := clitext.For(lang)
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
		return unexpectedArgument(lang, sub, flags.Arg(0))
	}
	cfg.HealthAddr = *healthAddr
	cfg.EnvFile = *envFile

	switch sub {
	case cmdInstall:
		cfg.DefaultCmd = *defaultCmd
		return service.Install(ctx, cfg)
	case "uninstall":
		if err := service.Uninstall(ctx, cfg); err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, render(lang, clitext.ServiceRemoved, nil))
		return nil
	case cmdStatus:
		return service.Status(ctx, cfg)
	default:
		return unknownSubcommand(lang, sub)
	}
}

func serviceUsageError(lang i18n.Language) error {
	return clitext.Failure(lang, errServiceUsage, clitext.ServiceUsage, nil)
}

func unknownSubcommand(lang i18n.Language, sub string) error {
	return clitext.Failure(lang, errUnknownSubcommand, clitext.ServiceUnknownSubcommand, clitext.Params{clitext.ParamName: strconv.Quote(sub)})
}

func unexpectedArgument(lang i18n.Language, sub, arg string) error {
	return clitext.Failure(lang, errUnexpectedArgument, clitext.ServiceUnexpectedArgument, clitext.Params{
		clitext.ParamVerb:  sub,
		clitext.ParamArg:   strconv.Quote(arg),
		clitext.ParamUsage: render(lang, clitext.ServiceUsage, nil),
	})
}

func runServiceUpdate(ctx context.Context, cfg service.Config, args []string) error {
	lang := clitext.ClientLanguage()
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	translator := clitext.For(lang)
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
		return clitext.Failure(lang, errUnexpectedArgument, clitext.ServiceUpdateUnexpectedArg, clitext.Params{clitext.ParamArg: strconv.Quote(flags.Arg(0))})
	}
	if installed, ok := service.InstalledBinPath(cfg); ok && installed != cfg.BinPath {
		fmt.Fprintln(os.Stderr, render(lang, clitext.ServiceUpdateRebuilding, clitext.Params{clitext.ParamInstalled: installed, clitext.ParamRan: cfg.BinPath}))
		cfg.BinPath = installed
	}
	if err := service.Update(ctx, cfg, *source, !*noPull); err != nil {
		return err
	}
	v := service.SourceVersion(ctx, *source)
	fmt.Fprintln(os.Stderr, render(lang, clitext.ServiceUpdateRebuilt, clitext.Params{clitext.ParamVersion: v}))
	return nil
}
