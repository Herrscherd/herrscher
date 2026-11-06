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
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Herrscherd/herrscher/internal/errwriter"

	"golang.org/x/term"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
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

	if err := clitext.For(clitext.ClientLanguage()).Err(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	if err := host.CaptureStartupSecrets(loadHostEnv, hideGatewayKeys); err != nil {
		return reportError(err)
	}

	ctx, stop := notifyStop(context.Background())
	defer stop()

	if len(os.Args) < minArgsWithCommand {
		return reportOutcome(ctx, runWithoutCommand(ctx))
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	if task, ok := promptOf(cmd, args); ok {
		if task.Text == "" {
			fmt.Fprintln(os.Stderr, render(clitext.ClientLanguage(), clitext.TaskNeedsText, clitext.Params{clitext.ParamVerb: cmd}))
			return exitUsage
		}
		return reportOutcome(ctx, runPrompt(ctx, task))
	}

	if code, ok := runManageCmd(cmd, args); ok {
		return code
	}

	switch cmd {
	case "-h", "--help", "help":
		if err := usage(clitext.ClientLanguage()); err != nil {
			return reportError(err)
		}
		return 0
	}

	return outcomeReporterFor(cmd)(ctx, runCommand(ctx, cmd, args))
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
	lang := clitext.ClientLanguage()
	if err := loadEnvFile(envPath, lang); err != nil {
		if explicit {
			return err
		}
		fmt.Fprintln(os.Stderr, render(lang, clitext.EnvContinuing, clitext.Params{clitext.ParamErr: err.Error()}))
	}
	return nil
}

func runWithoutCommand(ctx context.Context) error {
	if term.IsTerminal(int(os.Stdout.Fd())) && hasTerminalGateway() {
		return runServe(ctx, nil, nil)
	}
	if err := usage(clitext.ClientLanguage()); err != nil {
		return err
	}
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
	case "account":
		return runAccount(ctx, args)
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
	lang := clitext.ClientLanguage()
	out, code, derr := dispatchUnknown(ctx, lang, cmd, args, forwardUnknownVerb)
	if code != 0 {
		fmt.Fprintln(os.Stderr, render(lang, clitext.ErrorLine, clitext.Params{clitext.ParamMessage: derr.Error()}))
		if err := usage(lang); err != nil {
			return err
		}
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
	fmt.Fprintln(os.Stderr, render(clitext.ClientLanguage(), clitext.ErrorLine, clitext.Params{clitext.ParamMessage: err.Error()}))
	return 1
}

func dispatchUnknown(ctx context.Context, lang i18n.Language, cmd string, args []string, fwd func(context.Context, []string) (string, bool, error)) (string, int, error) {
	out, handled, derr := fwd(ctx, append([]string{cmd}, args...))
	if !handled {
		return "", exitUsage, renderError(lang, errUnknownCommand, clitext.CommandUnknown, clitext.Params{clitext.ParamName: strconv.Quote(cmd)})
	}
	return out, 0, derr
}

func channelFlag(flags *flag.FlagSet) *string {
	translator := clitext.For(clitext.ClientLanguage())
	channel := flags.String("channel", "", translator.Text(clitext.FlagChannel, nil))
	flags.StringVar(channel, "c", "", translator.Text(clitext.FlagChannelShort, nil))
	if err := translator.Err(); err != nil {
		slog.Error("channel flag help", "err", err)
	}
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
			return renderError(clitext.ClientLanguage(), errUnknownArgument, clitext.WhoamiUnknownArgument, clitext.Params{clitext.ParamArg: strconv.Quote(arg)})
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

func render(lang i18n.Language, key clitext.Key, params clitext.Params) string {
	translator := clitext.For(lang)
	text := translator.Text(key, params)
	if err := translator.Err(); err != nil {
		slog.Error("render message", "key", key, "err", err)
	}
	return text
}

func renderError(lang i18n.Language, cause error, key clitext.Key, params clitext.Params) error {
	text, err := clitext.Text(lang, key, params)
	if err != nil {
		return err
	}
	return renderedError{text: text, cause: cause}
}

type renderedError struct {
	text  string
	cause error
}

func (e renderedError) Error() string { return e.text }

func (e renderedError) Unwrap() error { return e.cause }
