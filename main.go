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

func main() {
	// GIT_ASKPASS re-entry: when forge.Clone authenticates a private HTTPS clone,
	// it points git's GIT_ASKPASS at this same binary and sets the marker below.
	// git then execs us with the credential prompt as argv[1] ("Username for…" /
	// "Password for…"). Answer and exit BEFORE any .env load or verb dispatch —
	// the token rides only the environment (GITHUB_TOKEN/GH_TOKEN), never argv or
	// disk. This branch is self-contained on purpose: main can't import the
	// internal forge package.
	if os.Getenv("HERRSCHER_GIT_ASKPASS") == "1" {
		prompt := ""
		if len(os.Args) > 1 {
			prompt = os.Args[1]
		}
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(prompt)), "username") {
			fmt.Println("x-access-token")
		} else if t := os.Getenv("GITHUB_TOKEN"); t != "" {
			fmt.Println(t)
		} else {
			fmt.Println(os.Getenv("GH_TOKEN"))
		}
		return
	}

	if len(os.Args) == 2 && (os.Args[1] == "version" || os.Args[1] == "--version") {
		fmt.Println(herrscherVersion())
		return
	}

	// Auto-load a project-root .env so every command (and every plugin's config
	// resolution) sees its vars without an explicit --env-file. Real environment
	// wins over the file, and the daemon propagates os.Environ() to each bridge
	// subprocess, so one .env floods gateway/backend/memory alike.
	//
	// $HERRSCHER_ENV_FILE overrides the path. We resolve to an ABSOLUTE path and
	// re-export it so bridge subprocesses — which run with cmd.Dir set to a
	// per-session worktree — load the *same* file, not a stray .env that happens
	// to sit in that worktree (which would be an env-injection vector). An
	// explicit $HERRSCHER_ENV_FILE is authoritative, so a load failure there is
	// fatal; the implicit ./.env is best-effort (a missing or unreadable file
	// must not break management verbs that need no secrets).
	envPath, explicit := os.Getenv("HERRSCHER_ENV_FILE"), true
	if envPath == "" {
		envPath, explicit = ".env", false
	}
	if abs, err := filepath.Abs(envPath); err == nil {
		envPath = abs
	}
	os.Setenv("HERRSCHER_ENV_FILE", envPath)
	if err := loadEnvFile(envPath); err != nil {
		if explicit {
			fmt.Fprintln(os.Stderr, "herrscher: "+err.Error())
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "herrscher: "+err.Error()+" (continuing)")
	}

	// Capture the Neublox gateway pair and REMOVE it from this process's
	// environment, before any verb runs. It is a product credential on a shared
	// paid account; leaving it in the environment would propagate it to every
	// vendor CLI a session spawns (backends spawn with MergeEnv(os.Environ(),
	// env)), handing a prompt-injectable coding agent `env | grep NEUBLOX`. This
	// is the earliest point common to every path — daemon, operator CLI, and the
	// supervised `herrscher bridge` child, which re-captures what the supervisor
	// hands it. It must run AFTER the .env load above, which may define the pair.
	host.CaptureGatewayCreds()

	ctx := context.Background()

	// Bare `herrscher`: open the terminal TUI when we can — an interactive TTY
	// plus a compiled-in terminal gateway, which runServe runs as its foreground
	// (see serve.go). Otherwise fall back to help. We deliberately never start a
	// background daemon from a bare, argument-less invocation, so `herrscher` in
	// a script (piped/redirected, no TTY) just prints usage and exits.
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

	// A free-text argv is a task, not a mistyped verb: open a session on it and
	// hand it the window. Checked here, ahead of every switch below, because the
	// two surfaces cannot collide — no verb in the registry contains whitespace.
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

	// Management verbs need no Discord client; dispatch them first. They shell out
	// to go get/tidy/build, so give them a context cancelled on Ctrl-C / SIGTERM to
	// stop those children cleanly. The runtime verbs keep their own lifecycle
	// (below), so signal handling stays scoped to management here.
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

	// The host stays gateway-agnostic: it never builds a Discord (dctl) client.
	// Every runtime verb drives the registered gateway plugin via the contracts
	// port; raw channel poking lives in the dctl library, not in this binary.
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
	case "memory":
		err = runMemory(ctx, args)
	case "models":
		err = runModels(ctx, args)
	case "service":
		err = runService(ctx, args)
	case "plugin-host":
		err = runPluginHost(ctx, args)
	case "worktree":
		// Answered here rather than forwarded to the daemon, like whoami: the
		// caller is a daemon on ANOTHER machine driving this binary over ssh, and
		// there is no daemon on this side to forward to.
		err = host.RunWorktree(ctx, args)
	case "whoami":
		// Answered here rather than forwarded to the daemon: it reads git, not
		// daemon state, and an operator reaches for it precisely when something
		// looks wrong — which may well be that no daemon is running.
		err = runWhoami(args)
	case "-h", "--help", "help":
		usage()
		return
	default:
		// The switch above is not the whole command surface: gateway plugins
		// contribute verbs the daemon namespaces under their kind, and those live
		// in the daemon's registry alone. So an argv this binary does not
		// recognise is offered to the daemon verbatim — main never learns what any
		// of them mean, which is what keeps this file free of any platform.
		// `herrscher commands` lists what the daemon will accept.
		//
		// Be honest about the reach: the daemon is handed the argv as-is, so what
		// this opens is its whole registry and not only the contributed part of
		// it — `herrscher set source --path /x` now lands on the live daemon
		// where it used to be an unknown verb. No privilege is added; the socket
		// already took arbitrary argv from anyone who could reach it.
		out, derr, code := dispatchUnknown(ctx, cmd, args, forwardUnknownVerb)
		if code != 0 {
			fmt.Fprintln(os.Stderr, "herrscher: "+derr.Error())
			usage()
			os.Exit(code)
		}
		if derr == nil && out != "" {
			fmt.Println(out)
		}
		err = derr
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "herrscher: "+err.Error())
		os.Exit(1)
	}
}

// dispatchUnknown decides what becomes of a verb main()'s switch has no case
// for. It lives outside main() so the decision can be exercised with a stub
// forwarder — no daemon, no re-exec — and it takes cmd and args apart rather
// than a ready-made argv so the arm passes exactly what every sibling case
// passes: dropping the verb on the wire is then not expressible at the call
// site. exit is the code the process must leave with, or 0 to let main() carry
// on with err as usual.
func dispatchUnknown(ctx context.Context, cmd string, args []string, fwd func(context.Context, []string) (string, bool, error)) (stdout string, err error, exit int) {
	out, handled, derr := fwd(ctx, append([]string{cmd}, args...))
	if !handled {
		return "", fmt.Errorf("unknown command %q", cmd), 2
	}
	// A daemon-side refusal is the caller's failure, so it must keep costing a
	// non-zero exit for whoever is reading $?.
	return out, derr, 0
}

// channelFlag registers -c/--channel on fs and returns the bound pointer.
func channelFlag(fs *flag.FlagSet) *string {
	ch := fs.String("channel", "", "conversation id this bridge answers in")
	fs.StringVar(ch, "c", "", "channel id (shorthand)")
	return ch
}

// hasTerminalGateway reports whether a terminal (TUI) gateway plugin is compiled
// in, so a bare `herrscher` invocation knows it can open the TUI. It inspects the
// plugin registry directly, without building the hub.
func hasTerminalGateway() bool {
	for _, p := range contracts.Default.Gateways() {
		if p.Manifest.Kind == "terminal" {
			return true
		}
	}
	return false
}

// runWhoami prints the identity git describes for this directory. It parses its
// own argv rather than going through core/cli, because it never reaches the
// registry — but it rejects what the registry would reject: a flag the verb does
// not define is a mistake worth naming, not an argument to ignore quietly.
func runWhoami(args []string) error {
	asJSON := false
	for _, a := range args {
		if a != "--json" {
			return fmt.Errorf("whoami: unknown argument %q (only --json)", a)
		}
		asJSON = true
	}
	out, err := host.WhoamiOut(asJSON)
	if err != nil {
		return err
	}
	fmt.Println(out)
	return nil
}
