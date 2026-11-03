// Command herrscher is the composition root and CLI for a Herrscher host: it wires
// the registered gateway/backend/orchestrator plugins and the core into one
// binary. It exposes the always-on daemon (serve/bridge/service), the session
// verbs, and the host self-management verbs (init/plugin/update/install). It
// stays gateway-agnostic: it never imports a concrete chat adapter (dctl lives
// in the discord-gateway plugin), driving platforms only through the contracts
// gateway port. Output is deliberately minimal so an LLM driving it spends few
// tokens.
//
// Config (env): the active gateway plugin declares its own required vars (the
// discord gateway needs DISCORD_BOT_TOKEN); the host resolves them generically.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/Herrscherd/herrscher/internal/errwriter"

	"golang.org/x/term"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/host"
	"github.com/Herrscherd/herrscher/manage"
)

var (
	errUnknownCommand  = errors.New("unknown command")
	errUnknownArgument = errors.New("unknown argument")
)

func main() {
	os.Exit(run())
}

func run() int {
	if os.Getenv("HERRSCHER_GIT_ASKPASS") == "1" {
		return answerGitAskpass(os.Args)
	}

	if len(os.Args) == 2 && (os.Args[1] == "version" || os.Args[1] == "--version") {
		out := errwriter.New(os.Stdout)
		out.Println(herrscherVersion())
		return writeFailureCode(out)
	}

	if err := host.CaptureStartupSecrets(loadHostEnv, hideGatewayKeys); err != nil {
		return reportError(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if len(os.Args) < minArgsWithCommand {
		return reportError(runWithoutCommand(ctx))
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	if task, ok := promptOf(cmd, args); ok {
		if task.Text == "" {
			fmt.Fprintf(os.Stderr, "herrscher: %s needs a task, e.g. herrscher %s refactor\n", cmd, cmd)
			return exitUsage
		}
		return reportError(runPrompt(ctx, task))
	}

	if code, ok := runManageCmd(cmd, args); ok {
		return code
	}

	switch cmd {
	case "-h", "--help", "help":
		usage()
		return 0
	}

	return reportError(runCommand(ctx, cmd, args))
}

func answerGitAskpass(args []string) int {
	prompt := ""
	if len(args) > 1 {
		prompt = args[1]
	}
	out := errwriter.New(os.Stdout)
	writeGitCredential(prompt, out)
	return writeFailureCode(out)
}

func writeGitCredential(prompt string, out *errwriter.Writer) {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(prompt)), "username") {
		out.Println("x-access-token")
		return
	}
	if t := os.Getenv("GITHUB_TOKEN"); t != "" {
		out.Println(t)
		return
	}
	out.Println(os.Getenv("GH_TOKEN"))
}

func loadHostEnv() error {
	envPath, explicit := os.Getenv("HERRSCHER_ENV_FILE"), true
	if envPath == "" {
		envPath, explicit = ".env", false
	}
	if abs, err := filepath.Abs(envPath); err == nil {
		envPath = abs
	}
	if err := os.Setenv("HERRSCHER_ENV_FILE", envPath); err != nil {
		return err
	}
	if err := loadEnvFile(envPath); err != nil {
		if explicit {
			return err
		}
		fmt.Fprintln(os.Stderr, "herrscher: "+err.Error()+" (continuing)")
	}
	return nil
}

func runWithoutCommand(ctx context.Context) error {
	if term.IsTerminal(int(os.Stdout.Fd())) && hasTerminalGateway() {
		return runServe(ctx, nil, nil)
	}
	usage()
	return exitStatusError{code: exitUsage}
}

func runManageCmd(cmd string, args []string) (int, bool) {
	switch cmd {
	case "init", "plugin", "update", "install":
	default:
		return 0, false
	}
	mctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	switch cmd {
	case "init":
		return manage.InitCmd(mctx, args), true
	case "plugin":
		return manage.PluginCmd(mctx, args), true
	case "update":
		return manage.UpdateCmd(mctx, args), true
	default:
		return manage.InstallCmd(mctx, args), true
	}
}

func runCommand(ctx context.Context, cmd string, args []string) error {
	switch cmd {
	case "bridge":
		return runBridge(ctx, args)
	case "serve":
		return runServe(ctx, args, nil)
	case "session":
		return runSession(ctx, args)
	case "agent":
		return runAgent(ctx, args)
	case "host":
		return runHost(ctx, args)
	case "approve":
		return runApprove(ctx, args)
	case "role":
		return runRole(ctx, args)
	case "memory":
		return runMemory(ctx, args)
	case "models":
		return runModels(ctx, args)
	case "service":
		return runService(ctx, args)
	case "plugin-host":
		return runPluginHost(ctx, args)
	case "worktree":
		return host.RunWorktree(ctx, args)
	case "whoami":
		return runWhoami(ctx, args)
	}
	return runForwarded(ctx, cmd, args)
}

func runForwarded(ctx context.Context, cmd string, args []string) error {
	out, code, derr := dispatchUnknown(ctx, cmd, args, forwardUnknownVerb)
	if code != 0 {
		fmt.Fprintln(os.Stderr, "herrscher: "+derr.Error())
		usage()
		return exitStatusError{code: code}
	}
	if derr != nil {
		return derr
	}
	if out == "" {
		return nil
	}
	_, err := fmt.Fprintln(os.Stdout, out)
	return err
}

type exitStatusError struct {
	code int
}

func (s exitStatusError) Error() string {
	return fmt.Sprintf("exit status %d", s.code)
}

func reportError(err error) int {
	if err == nil {
		return 0
	}
	var status exitStatusError
	if errors.As(err, &status) {
		return status.code
	}
	fmt.Fprintln(os.Stderr, "herrscher: "+err.Error())
	return 1
}

func dispatchUnknown(ctx context.Context, cmd string, args []string, fwd func(context.Context, []string) (string, bool, error)) (string, int, error) {
	out, handled, derr := fwd(ctx, append([]string{cmd}, args...))
	if !handled {
		return "", exitUsage, fmt.Errorf("%w %q", errUnknownCommand, cmd)
	}
	return out, 0, derr
}

func channelFlag(flags *flag.FlagSet) *string {
	channel := flags.String("channel", "", "conversation id this bridge answers in")
	flags.StringVar(channel, "c", "", "channel id (shorthand)")
	return channel
}

func hasTerminalGateway() bool {
	gateways := contracts.Default.Gateways()
	for i := range gateways {
		p := &gateways[i]
		if p.Manifest.Kind == kindTerminal {
			return true
		}
	}
	return false
}

func runWhoami(ctx context.Context, args []string) error {
	asJSON := false
	for _, arg := range args {
		if arg != argJSON {
			return fmt.Errorf("whoami: %w %q (only --json)", errUnknownArgument, arg)
		}
		asJSON = true
	}
	out, err := host.WhoamiOut(ctx, asJSON)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(os.Stdout, out)
	return err
}

func writeFailureCode(w *errwriter.Writer) int {
	if w.Err() != nil {
		return 1
	}
	return 0
}
