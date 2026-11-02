package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func usage() {
	r := lipgloss.NewRenderer(os.Stderr)
	var (
		banner  = r.NewStyle().Bold(true).Foreground(lipgloss.Color("13"))
		tagline = r.NewStyle().Faint(true)
		section = r.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
		verb    = r.NewStyle().Bold(true)
		hint    = r.NewStyle().Faint(true)
	)

	const col = 34
	row := func(v, h string) string {
		pad := col - len([]rune(v))
		if pad < 1 {
			pad = 1
		}
		return "    " + verb.Render(v) + strings.Repeat(" ", pad) + hint.Render(h)
	}

	group := func(title string, rows ...string) string {
		return "  " + section.Render(title) + "\n" + strings.Join(rows, "\n")
	}

	blocks := []string{
		"  " + banner.Render("⛧ HERRSCHER"),
		"  " + tagline.Render("modular Discord ⇄ Claude agent harness host"),
		"",
		group("DÉMARRER",
			row("herrscher \"<texte>\"", "open a session on this task, in the TUI"),
			row("herrscher --print \"<texte>\"", "same, but run one turn and print the reply"),
			row("herrscher", "open the multi-session terminal TUI"),
			row("herrscher version", "print the build version"),
			row("herrscher init", "compose the plugin stack + secrets (wizard)"),
		),
		"",
		group("SESSIONS & AGENTS",
			row("session <create|close|list|who>", "bridged channel + worktree + backend"),
			row("agent   <create|list>", "durable companion agents (persona/MCP)"),
			row("host    <add|list|check|provision|rm>", "machines a session can run on (`session create --host`)"),
			row("approve <list|allow|deny|rule|mode>", "actions that need a human's go-ahead before they run"),
			row("memory  <locate|forget|record>", "inspect/edit the memory graph"),
			row("models  list", "list the models the compiled backends offer"),
		),
		"",
		group("DAEMON & SERVICE",
			row("serve", "always-on gateway daemon (24/7)"),
			row("bridge  --cmd '<command>'", "link a channel to one command"),
			row("service <install|uninstall|…>", "run serve as a native boot service"),
			row("commands", "every verb the running daemon accepts (needs `serve`)"),
		),
		"",
		group("SETUP & MAINTENANCE",
			row("plugin  <list|add|remove|pin|unpin>", "edit the compiled-in plugin set + rebuild"),
			row("update", "bump every unpinned plugin + rebuild + reinstall"),
			row("install", "build then run the service install"),
		),
		"",
		"  " + hint.Render("Run `herrscher <command> --help` for flags and options."),
		"",
		"  " + hint.Render("env: DISCORD_BOT_TOKEN (required), DISCORD_USER_ID (the user the bot obeys)"),
		"  " + hint.Render("     HERRSCHER_OWNER_ID (instance-id fallback), HERRSCHER_STATE_DIR (state dir)"),
		"  " + hint.Render("     HERRSCHER_ROUTE_POLICY=gateway-only (n'offre que les modèles passerelle)"),
		"  " + hint.Render("     NEUBLOX_GATEWAY_URL + NEUBLOX_GATEWAY_TOKEN (les deux, sinon une session passerelle échoue)"),
		"  " + hint.Render("     HERRSCHER_DEFAULT_MODEL (modèle pris quand `session create` n'en nomme aucun)"),
		"  " + hint.Render("     `herrscher init` pose ces trois questions — pas besoin de les écrire à la main"),
	}

	fmt.Fprintln(os.Stderr, strings.Join(blocks, "\n"))
}
