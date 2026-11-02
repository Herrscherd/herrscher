package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/Herrscherd/herrscher/core/service"
)

func runService(ctx context.Context, args []string) error {
	const usage = "usage: herrscher service <install|uninstall|status|restart|update> [--health-addr ADDR] [--env-file PATH] [--cmd 'claude …'] [--source DIR] [--no-pull]"
	if len(args) == 0 {
		return errors.New(usage)
	}
	sub := args[0]
	switch sub {
	case "install", "uninstall", "status", "restart", "update":
	default:
		return fmt.Errorf("herrscher service: unknown subcommand %q (want install|uninstall|status|restart|update)", sub)
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
		fmt.Fprintln(os.Stderr, "herrscher service: restarted")
		return nil
	case "update":
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
		return fmt.Errorf("herrscher service %s: unexpected argument %q\n%s", sub, fs.Arg(0), usage)
	}
	cfg.HealthAddr = *healthAddr
	cfg.EnvFile = *envFile

	switch sub {
	case "install":
		cfg.DefaultCmd = *defaultCmd
		return service.Install(ctx, cfg)
	case "uninstall":
		if err := service.Uninstall(ctx, cfg); err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, "herrscher service: removed")
		return nil
	case "status":
		return service.Status(ctx, cfg)
	default:
		return fmt.Errorf("herrscher service: unknown subcommand %q (want install|uninstall|status)", sub)
	}
}

func runServiceUpdate(ctx context.Context, cfg service.Config, args []string) error {
	cwd, _ := os.Getwd()
	fs := flag.NewFlagSet("service update", flag.ContinueOnError)
	source := fs.String("source", cwd, "path to the herrscher source checkout to build from")
	noPull := fs.Bool("no-pull", false, "skip `git pull --ff-only` before building")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("herrscher service update: unexpected argument %q", fs.Arg(0))
	}
	if installed, ok := service.InstalledBinPath(cfg); ok && installed != cfg.BinPath {
		fmt.Fprintf(os.Stderr, "herrscher service: rebuilding the installed binary %s (you ran %s)\n", installed, cfg.BinPath)
		cfg.BinPath = installed
	}
	if err := service.Update(ctx, cfg, *source, !*noPull); err != nil {
		return err
	}
	v := service.SourceVersion(ctx, *source)
	fmt.Fprintf(os.Stderr, "herrscher service: rebuilt %s and restarted\n", v)
	return nil
}
