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

	ctx := context.Background()

	// Bare `herrscher`: open the terminal TUI when we can — an interactive TTY
	// plus a compiled-in terminal gateway, which runServe runs as its foreground
	// (see serve.go). Otherwise fall back to help. We deliberately never start a
	// background daemon from a bare, argument-less invocation, so `herrscher` in
	// a script (piped/redirected, no TTY) just prints usage and exits.
	if len(os.Args) < 2 {
		if term.IsTerminal(int(os.Stdout.Fd())) && hasTerminalGateway() {
			if err := runServe(ctx, nil); err != nil {
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
		err = runServe(ctx, args)
	case "session":
		err = runSession(ctx, args)
	case "agent":
		err = runAgent(ctx, args)
	case "memory":
		err = runMemory(ctx, args)
	case "service":
		err = runService(ctx, args)
	case "plugin-host":
		err = runPluginHost(ctx, args)
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "herrscher: unknown command %q\n", cmd)
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "herrscher: "+err.Error())
		os.Exit(1)
	}
}

// channelFlag registers -c/--channel on fs and returns the bound pointer.
func channelFlag(fs *flag.FlagSet) *string {
	ch := fs.String("channel", "", "channel id (default: DISCORD_CHANNEL_ID)")
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
