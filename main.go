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
	if os.Getenv("HERRSCHER_GIT_ASKPASS") == "1" {
		prompt := ""
		if len(os.Args) > 1 {
			prompt = os.Args[1]
		}
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(prompt)), "username") {
			fmt.Fprintln(os.Stdout, "x-access-token")
		} else if t := os.Getenv("GITHUB_TOKEN"); t != "" {
			fmt.Fprintln(os.Stdout, t)
		} else {
			fmt.Fprintln(os.Stdout, os.Getenv("GH_TOKEN"))
		}
		return
	}

	if len(os.Args) == 2 && (os.Args[1] == "version" || os.Args[1] == "--version") {
		fmt.Fprintln(os.Stdout, herrscherVersion())
		return
	}

	envPath, explicit := os.Getenv("HERRSCHER_ENV_FILE"), true
	if envPath == "" {
		envPath, explicit = ".env", false
	}
	if abs, err := filepath.Abs(envPath); err == nil {
		envPath = abs
	}
	if err := os.Setenv("HERRSCHER_ENV_FILE", envPath); err != nil {
		fmt.Fprintln(os.Stderr, "herrscher: "+err.Error())
		os.Exit(1)
	}
	if err := loadEnvFile(envPath); err != nil {
		if explicit {
			fmt.Fprintln(os.Stderr, "herrscher: "+err.Error())
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "herrscher: "+err.Error()+" (continuing)")
	}

	host.CaptureGatewayCreds()
	hideGatewayKeys()

	ctx := context.Background()

	if len(os.Args) < 2 {
		if term.IsTerminal(int(os.Stdout.Fd())) && hasTerminalGateway() {
			if err := runServe(ctx, nil, nil); err != nil {
				fmt.Fprintln(os.Stderr, "herrscher: "+err.Error())
				os.Exit(1)
			}
			return
		}
		usage()
		os.Exit(2)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	if t, ok := promptOf(cmd, args); ok {
		if t.Text == "" {
			fmt.Fprintf(os.Stderr, "herrscher: %s needs a task, e.g. herrscher %s refactor\n", cmd, cmd)
			os.Exit(2)
		}
		if err := runPrompt(ctx, t); err != nil {
			fmt.Fprintln(os.Stderr, "herrscher: "+err.Error())
			os.Exit(1)
		}
		return
	}

	switch cmd {
	case "init", "plugin", "update", "install":
		mctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		var code int
		switch cmd {
		case "init":
			code = manage.InitCmd(mctx, args)
		case "plugin":
			code = manage.PluginCmd(mctx, args)
		case "update":
			code = manage.UpdateCmd(mctx, args)
		case "install":
			code = manage.InstallCmd(mctx, args)
		}
		stop()
		os.Exit(code)
	}

	var err error
	switch cmd {
	case "bridge":
		err = runBridge(ctx, args)
	case "serve":
		err = runServe(ctx, args, nil)
	case "session":
		err = runSession(ctx, args)
	case "agent":
		err = runAgent(ctx, args)
	case "host":
		err = runHost(ctx, args)
	case "approve":
		err = runApprove(ctx, args)
	case "role":
		err = runRole(ctx, args)
	case "memory":
		err = runMemory(ctx, args)
	case "models":
		err = runModels(ctx, args)
	case "service":
		err = runService(ctx, args)
	case "plugin-host":
		err = runPluginHost(ctx, args)
	case "worktree":
		err = host.RunWorktree(ctx, args)
	case "whoami":
		err = runWhoami(args)
	case "-h", "--help", "help":
		usage()
		return
	default:
		out, code, derr := dispatchUnknown(ctx, cmd, args, forwardUnknownVerb)
		if code != 0 {
			fmt.Fprintln(os.Stderr, "herrscher: "+derr.Error())
			usage()
			os.Exit(code)
		}
		if derr == nil && out != "" {
			fmt.Fprintln(os.Stdout, out)
		}
		err = derr
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "herrscher: "+err.Error())
		os.Exit(1)
	}
}

func dispatchUnknown(ctx context.Context, cmd string, args []string, fwd func(context.Context, []string) (string, bool, error)) (string, int, error) {
	out, handled, derr := fwd(ctx, append([]string{cmd}, args...))
	if !handled {
		return "", 2, fmt.Errorf("%w %q", errUnknownCommand, cmd)
	}
	return out, 0, derr
}

func channelFlag(fs *flag.FlagSet) *string {
	ch := fs.String("channel", "", "conversation id this bridge answers in")
	fs.StringVar(ch, "c", "", "channel id (shorthand)")
	return ch
}

func hasTerminalGateway() bool {
	for _, p := range contracts.Default.Gateways() {
		if p.Manifest.Kind == "terminal" {
			return true
		}
	}
	return false
}

func runWhoami(args []string) error {
	asJSON := false
	for _, a := range args {
		if a != "--json" {
			return fmt.Errorf("whoami: %w %q (only --json)", errUnknownArgument, a)
		}
		asJSON = true
	}
	out, err := host.WhoamiOut(asJSON)
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, out)
	return nil
}
