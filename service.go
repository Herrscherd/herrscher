package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/Herrscherd/herrscher/core/service"
)

const serviceUsage = "usage: herrscher service <install|uninstall|status|restart|update> [--health-addr ADDR] [--env-file PATH] [--cmd 'claude …'] [--source DIR] [--no-pull]"

var (
	errServiceUsage       = errors.New(serviceUsage)
	errUnknownSubcommand  = errors.New("unknown subcommand")
	errUnexpectedArgument = errors.New("unexpected argument")
)

func runService(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errServiceUsage
	}
	sub := args[0]
	switch sub {
	case cmdInstall, "uninstall", cmdStatus, "restart", cmdUpdate:
	default:
		return fmt.Errorf("herrscher service: %w %q (want install|uninstall|status|restart|update)", errUnknownSubcommand, sub)
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
		_, _ = fmt.Fprintln(os.Stderr, "herrscher service: restarted")
		return nil
	case cmdUpdate:
		return runServiceUpdate(ctx, cfg, args[1:])
	}

	fs := flag.NewFlagSet("service", flag.ContinueOnError)
	healthAddr := fs.String("health-addr", cfg.HealthAddr, "value for serve --health-addr (empty disables the health endpoint)")
	envFile := fs.String("env-file", cfg.EnvFile, "path to the 0600 secrets file the service sources")
	defaultCmd := fs.String("cmd", "", "default bridged command pre-filled into config.json (e.g. 'claude --model claude-opus-4-8 --effort low')")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("herrscher service %s: %w %q\n%s", sub, errUnexpectedArgument, fs.Arg(0), serviceUsage)
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
		_, _ = fmt.Fprintln(os.Stderr, "herrscher service: removed")
		return nil
	case cmdStatus:
		return service.Status(ctx, cfg)
	default:
		return fmt.Errorf("herrscher service: %w %q (want install|uninstall|status)", errUnknownSubcommand, sub)
	}
}

func runServiceUpdate(ctx context.Context, cfg service.Config, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("service update", flag.ContinueOnError)
	source := fs.String("source", cwd, "path to the herrscher source checkout to build from")
	noPull := fs.Bool("no-pull", false, "skip `git pull --ff-only` before building")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("herrscher service update: %w %q", errUnexpectedArgument, fs.Arg(0))
	}
	if installed, ok := service.InstalledBinPath(cfg); ok && installed != cfg.BinPath {
		_, _ = fmt.Fprintf(os.Stderr, "herrscher service: rebuilding the installed binary %s (you ran %s)\n", installed, cfg.BinPath)
		cfg.BinPath = installed
	}
	if err := service.Update(ctx, cfg, *source, !*noPull); err != nil {
		return err
	}
	v := service.SourceVersion(ctx, *source)
	_, _ = fmt.Fprintf(os.Stderr, "herrscher service: rebuilt %s and restarted\n", v)
	return nil
}
