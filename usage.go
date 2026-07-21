package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// usage prints the top-level help. It is grouped by intent and styled with
// lipgloss; the renderer is bound to os.Stderr so color auto-degrades to plain
// text under NO_COLOR, TERM=dumb, or a non-TTY stderr — matching the convention
// in manage/style.go. Help goes to stderr (unchanged), so `2>` redirection and
// existing scripts keep working.
func usage() {
	r := lipgloss.NewRenderer(os.Stderr)
	var (
		banner  = r.NewStyle().Bold(true).Foreground(lipgloss.Color("13"))
		tagline = r.NewStyle().Faint(true)
		section = r.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
		verb    = r.NewStyle().Bold(true)
		hint    = r.NewStyle().Faint(true)
	)

	// row renders one "  <verb>  <hint>" line with the verb column padded so the
	// hints align. col is the verb-column width (in visible runes).
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
			row("herrscher", "open the multi-session terminal TUI"),
			row("herrscher version", "print the build version"),
			row("herrscher init", "compose the plugin stack + secrets (wizard)"),
		),
		"",
		group("SESSIONS & AGENTS",
			row("session <create|close|list|who>", "bridged channel + worktree + backend"),
			row("agent   <create|list>", "durable companion agents (persona/MCP)"),
			row("memory  <locate|forget|record>", "inspect/edit the memory graph"),
		),
		"",
		group("DAEMON & SERVICE",
			row("serve", "always-on gateway daemon (24/7)"),
			row("bridge  --cmd '<command>'", "link a channel to one command"),
			row("service <install|uninstall|…>", "run serve as a native boot service"),
		),
		"",
		group("SETUP & MAINTENANCE",
			row("plugin  <list|add|remove>", "edit the compiled-in plugin set + rebuild"),
			row("update", "bump every plugin + rebuild"),
			row("install", "build then run the service install"),
		),
		"",
		"  " + hint.Render("Run `herrscher <command> --help` for flags and options."),
		"",
		"  " + hint.Render("env: DISCORD_BOT_TOKEN (required), DISCORD_CHANNEL_ID (default channel)"),
		"  " + hint.Render("     HERRSCHER_OWNER_ID (instance-id fallback), HERRSCHER_STATE_DIR (state dir)"),
	}

	fmt.Fprintln(os.Stderr, strings.Join(blocks, "\n"))
}
