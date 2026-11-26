package manage

import (
	"context"
	"errors"
	"flag"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/envx"
	"github.com/Herrscherd/herrscher/core/host"
	"github.com/Herrscherd/herrscher/internal/errwriter"
)

var (
	errUnknownSection = clitext.SentinelError("unknown setup section")
	errNeedsTerminal  = clitext.SentinelError("setup needs an interactive terminal")
)

func setupSections() map[string]setupScreen {
	return map[string]setupScreen{
		"modules":       screenModules,
		kindDiscord:     screenDiscord,
		kindTelegram:    screenTelegram,
		kindClaude:      screenClaude,
		kindCodex:       screenCodex,
		kindCursor:      screenCursor,
		kindGrok:        screenGrok,
		kindAntigravity: screenAntigravity,
		kindObsidian:    screenObsidian,
		"route":         screenRoute,
		"connections":   screenConnections,
	}
}

func sectionList() string {
	names := slices.Sorted(maps.Keys(setupSections()))
	return strings.Join(names, "|")
}

// SetupCmd opens the interactive setup: it picks the modules, configures each of
// them (signing in to Claude and Codex), writes the settings and, on a source
// host, rewrites plugins.go and rebuilds. An optional section name opens that
// screen directly; an existing settings file opens the dashboard.
func SetupCmd(ctx context.Context, messages *clitext.Messages, hostMessages *host.Messages, args []string) int {
	out, errOut := errwriter.New(os.Stdout), errwriter.New(os.Stderr)
	return errwriter.Finish(setupHost(ctx, messages, hostMessages, errOut, args), out, errOut)
}

func setupHost(ctx context.Context, messages *clitext.Messages, hostMessages *host.Messages, errOut *errwriter.Writer, args []string) int {
	flags := flag.NewFlagSet("setup", flag.ContinueOnError)
	hostDir := flags.String("host", "", msg(messages, clitext.ManageFlagHost, nil))
	if err := flags.Parse(args); err != nil {
		return parseExit(err)
	}
	screen, err := setupEntry(messages, flags.Args())
	if err != nil {
		errOut.Println(err)
		return exitUsage
	}
	if !isTerminal(os.Stdin) || !isTerminal(os.Stdout) {
		errOut.Println(failure(messages, errNeedsTerminal, clitext.SetupNeedsTerminal, nil))
		return 1
	}
	deps := liveDeps(messages, hostMessages, *hostDir)
	return runSetup(ctx, messages, errOut, deps, screen)
}

func setupEntry(messages *clitext.Messages, rest []string) (setupScreen, error) {
	switch len(rest) {
	case 0:
		if _, err := os.Stat(envTarget()); err == nil {
			return screenHub, nil
		}
		return screenWelcome, nil
	case 1:
		if screen, ok := setupSections()[rest[0]]; ok {
			return screen, nil
		}
		return screenWelcome, failure(messages, errUnknownSection, clitext.SetupUnknownSection, clitext.Params{clitext.ParamName: rest[0]})
	}
	return screenWelcome, failure(messages, errUnknownSection, clitext.SetupUsage, clitext.Params{clitext.ParamName: sectionList()})
}

func liveDeps(messages *clitext.Messages, hostMessages *host.Messages, hostDir string) setupDeps {
	deps := setupDeps{
		prober:      liveProber(),
		github:      liveGitHubLink(),
		login:       execLogin,
		catalog:     liveCatalog(host.DefaultModelDiscovery(hostMessages)),
		run:         execRunner,
		envPath:     envTarget(),
		abandonWait: abandonLimit,
		trialWait:   trialTimeout,
	}
	dir, err := resolveHost(messages, hostDir)
	if err != nil {
		return deps
	}
	manifest, err := os.ReadFile(filepath.Join(dir, manifestFile))
	if err != nil {
		return deps
	}
	if abs, absErr := filepath.Abs(dir); absErr == nil {
		dir = abs
	}
	deps.hostDir, deps.manifest, deps.binary = dir, manifest, runningBinary(dir)
	return deps
}

func runSetup(ctx context.Context, messages *clitext.Messages, errOut *errwriter.Writer, deps setupDeps, screen setupScreen) int {
	_, statErr := os.Stat(deps.envPath)
	model := newSetupModel(messages, deps, setupStart{ctx: ctx, getenv: envx.Getenv, screen: screen, envExists: statErr == nil})
	_, err := tea.NewProgram(model, tea.WithContext(ctx)).Run()
	model.abandon()
	if err != nil && !errors.Is(err, tea.ErrProgramKilled) {
		errOut.Println(failure(messages, err, clitext.SetupFailed, clitext.Params{clitext.ParamErr: err.Error()}))
		return 1
	}
	if model.install.cleanupErr != nil {
		errOut.Println(model.install.cleanupErr)
	}
	if model.install.err != nil || !model.install.trialsAnswered() {
		return 1
	}
	return 0
}

// FirstRun reports whether this machine has neither a settings file nor a host
// state yet, the case where a bare herrscher opens the setup.
func FirstRun() bool {
	for _, path := range []string{envTarget(), host.DefaultStatePath()} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			return false
		}
	}
	return true
}
