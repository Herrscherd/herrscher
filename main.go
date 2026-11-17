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
	"io"
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
	return runWith(host.ProductionRuntime())
}

func runWith(layout host.RuntimeLayout) int {
	if os.Getenv("HERRSCHER_GIT_ASKPASS") == "1" {
		return answerGitAskpass(os.Args)
	}

	if len(os.Args) == 2 && (os.Args[1] == "version" || os.Args[1] == "--version") {
		out := errwriter.New(os.Stdout)
		out.Println(herrscherVersion())
		return writeFailureCode(out)
	}

	messages := clitext.NewMessages()
	hostMessages := host.NewMessages()
	if refuseBrokenCatalogs(messages, os.Stderr) {
		return 1
	}

	if err := host.CaptureStartupSecrets(func() error { return loadHostEnv(messages) }, hideGatewayKeys); err != nil {
		return reportError(messages, err)
	}

	ctx, stop := notifyStop(context.Background())
	defer stop()

	if len(os.Args) < minArgsWithCommand {
		return reportOutcome(ctx, messages, runWithoutCommand(ctx, layout, messages, hostMessages))
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	if task, ok := promptOf(cmd, args); ok {
		if task.Text == "" {
			fmt.Fprintln(os.Stderr, render(messages, clitext.ClientLanguage(), clitext.TaskNeedsText, clitext.Params{clitext.ParamVerb: cmd}))
			return exitUsage
		}
		return reportOutcome(ctx, messages, runPrompt(ctx, layout, messages, hostMessages, task))
	}

	if code, ok := runManageCmd(messages, hostMessages, cmd, args); ok {
		return code
	}

	switch cmd {
	case "-h", "--help", "help":
		if err := usage(messages, clitext.ClientLanguage()); err != nil {
			return reportError(messages, err)
		}
		return 0
	}

	return outcomeReporterFor(messages, cmd)(ctx, runCommand(ctx, layout, messages, hostMessages, cmd, args))
}

func refuseBrokenCatalogs(messages *clitext.Messages, stderr io.Writer) bool {
	err := messages.Err()
	if err == nil {
		return false
	}
	errwriter.New(stderr).Println(err)
	return true
}

func answerGitAskpass(args []string) int {
	prompt := ""
	if len(args) > 1 {
		prompt = args[1]
	}
	out := errwriter.New(os.Stdout)
	if err := writeGitCredential(prompt, out); err != nil {
		return 1
	}
	return writeFailureCode(out)
}

func writeGitCredential(prompt string, out *errwriter.Writer) error {
	_, err := out.Write([]byte(gitCredential(prompt) + "\n"))
	return err
}

func gitCredential(prompt string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(prompt)), "username") {
		return "x-access-token"
	}
	if t := os.Getenv("GITHUB_TOKEN"); t != "" {
		return t
	}
	return os.Getenv("GH_TOKEN")
}

func loadHostEnv(messages *clitext.Messages) error {
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
	if err := loadEnvFile(messages, envPath, lang); err != nil {
		if explicit {
			return err
		}
		fmt.Fprintln(os.Stderr, cliErrorLine(render(messages, lang, clitext.EnvContinuing, clitext.Params{clitext.ParamErr: err.Error()})))
	}
	return nil
}

func runWithoutCommand(ctx context.Context, layout host.RuntimeLayout, messages *clitext.Messages, hostMessages *host.Messages) error {
	if term.IsTerminal(int(os.Stdout.Fd())) && hasTerminalGateway() {
		return runServe(ctx, layout, messages, hostMessages, nil, serveDaemon)
	}
	if err := usage(messages, clitext.ClientLanguage()); err != nil {
		return err
	}
	return exitStatusError{code: exitUsage}
}

func runManageCmd(messages *clitext.Messages, hostMessages *host.Messages, cmd string, args []string) (int, bool) {
	switch cmd {
	case "init", "plugin", "update", "install":
	default:
		return 0, false
	}
	mctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	switch cmd {
	case "init":
		return manage.InitCmd(mctx, messages, hostMessages, args), true
	case "plugin":
		return manage.PluginCmd(mctx, messages, args), true
	case "update":
		return manage.UpdateCmd(mctx, messages, args), true
	default:
		return manage.InstallCmd(mctx, messages, args), true
	}
}

func runCommand(ctx context.Context, layout host.RuntimeLayout, messages *clitext.Messages, hostMessages *host.Messages, cmd string, args []string) error {
	switch cmd {
	case "bridge":
		return runBridge(ctx, messages, hostMessages, args)
	case "serve":
		return runServe(ctx, layout, messages, hostMessages, args, serveDaemon)
	case "session":
		return runSession(ctx, layout, messages, hostMessages, args)
	case "agent":
		return runAgent(ctx, layout, messages, hostMessages, args)
	case "account":
		return runAccount(ctx, messages, hostMessages, args)
	case "host":
		return runHost(ctx, layout, messages, hostMessages, args)
	case "approve":
		return runApprove(ctx, layout, messages, hostMessages, args)
	case "role":
		return runRole(ctx, layout, messages, hostMessages, args)
	case "memory":
		return runMemory(ctx, layout, messages, hostMessages, args)
	case "models":
		return runModels(ctx, layout, messages, hostMessages, args)
	case "service":
		return runService(ctx, messages, args)
	case "plugin-host":
		return runPluginHost(ctx, messages, hostMessages, args)
	case "worktree":
		return host.RunWorktree(ctx, messages, hostMessages, args)
	case healthVerb:
		return runHealth(ctx, layout, messages, hostMessages, args)
	case usageVerb:
		return runUsage(ctx, layout, messages, hostMessages, args)
	case "whoami":
		return runWhoami(ctx, messages, args)
	}
	return runForwarded(ctx, layout, messages, hostMessages, cmd, args)
}

func runForwarded(ctx context.Context, layout host.RuntimeLayout, messages *clitext.Messages, hostMessages *host.Messages, cmd string, args []string) error {
	lang := clitext.ClientLanguage()
	out, code, derr := dispatchUnknown(ctx, messages, lang, cmd, args, func(ctx context.Context, argv []string) (string, bool, error) {
		return forwardUnknownVerb(ctx, layout, hostMessages, argv)
	})
	if code != 0 {
		fmt.Fprintln(os.Stderr, forwardedFailureLine(messages, hostMessages, lang, derr))
		if err := usage(messages, lang); err != nil {
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
	return writeCLIText(os.Stdout, out)
}

func forwardedFailureLine(messages *clitext.Messages, hostMessages *host.Messages, lang i18n.Language, derr error) string {
	return cliErrorLine(render(messages, lang, clitext.ErrorLine, clitext.Params{clitext.ParamMessage: hostMessages.Localize(messages, lang, derr).Error()}))
}

type exitStatusError struct {
	code int
}

func (s exitStatusError) Error() string {
	return fmt.Sprintf("exit status %d", s.code)
}

func reportError(messages *clitext.Messages, err error) int {
	return reportErrorTo(messages, os.Stderr, err)
}

func reportErrorTo(messages *clitext.Messages, stderr io.Writer, err error) int {
	if err == nil {
		return 0
	}
	var status exitStatusError
	if errors.As(err, &status) {
		return status.code
	}
	line := cliErrorLine(render(messages, clitext.ClientLanguage(), clitext.ErrorLine, clitext.Params{clitext.ParamMessage: err.Error()}))
	if _, writeErr := fmt.Fprintln(stderr, line); writeErr != nil {
		slog.Warn("write error line", "err", writeErr)
	}
	return 1
}

func dispatchUnknown(ctx context.Context, messages *clitext.Messages, lang i18n.Language, cmd string, args []string, fwd func(context.Context, []string) (string, bool, error)) (string, int, error) {
	out, handled, derr := fwd(ctx, append([]string{cmd}, args...))
	if !handled {
		return "", exitUsage, renderError(messages, lang, errUnknownCommand, clitext.CommandUnknown, clitext.Params{clitext.ParamName: strconv.Quote(cmd)})
	}
	return out, 0, derr
}

func channelFlag(messages *clitext.Messages, flags *flag.FlagSet) *string {
	translator := messages.For(clitext.ClientLanguage())
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

func runWhoami(ctx context.Context, messages *clitext.Messages, args []string) error {
	asJSON := false
	for _, arg := range args {
		if arg != argJSON {
			return renderError(messages, clitext.ClientLanguage(), errUnknownArgument, clitext.WhoamiUnknownArgument, clitext.Params{clitext.ParamArg: strconv.Quote(arg)})
		}
		asJSON = true
	}
	out, err := host.WhoamiOut(ctx, asJSON)
	if err != nil {
		return err
	}
	return writeCLIText(os.Stdout, out)
}

func writeFailureCode(w *errwriter.Writer) int {
	if w.Err() != nil {
		return 1
	}
	return 0
}

func render(messages *clitext.Messages, lang i18n.Language, key clitext.Key, params clitext.Params) string {
	translator := messages.For(lang)
	text := translator.Text(key, clitext.Printable(params))
	if err := translator.Err(); err != nil {
		slog.Error("render message", "key", key, "err", err)
	}
	return text
}

func renderError(messages *clitext.Messages, lang i18n.Language, cause error, key clitext.Key, params clitext.Params) error {
	text, err := messages.Text(lang, key, params)
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
