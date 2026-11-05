package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/charmbracelet/lipgloss"

	"github.com/Herrscherd/herrscher/core/clitext"
)

func usage(lang i18n.Language) error {
	renderer := lipgloss.NewRenderer(os.Stderr)
	var (
		banner  = renderer.NewStyle().Bold(true).Foreground(lipgloss.Color("13"))
		tagline = renderer.NewStyle().Faint(true)
		section = renderer.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
		verb    = renderer.NewStyle().Bold(true)
		hint    = renderer.NewStyle().Faint(true)
	)
	tr := clitext.For(lang)

	const col = 34
	row := func(value string, help clitext.Key) string {
		pad := col - len([]rune(value))
		if pad < 1 {
			pad = 1
		}
		return "    " + verb.Render(value) + strings.Repeat(" ", pad) + hint.Render(tr.Text(help, nil))
	}

	group := func(title clitext.Key, rows ...string) string {
		return "  " + section.Render(tr.Text(title, nil)) + "\n" + strings.Join(rows, "\n")
	}

	blocks := []string{
		"  " + banner.Render("⛧ HERRSCHER"),
		"  " + tagline.Render(tr.Text(clitext.UsageTagline, nil)),
		"",
		group(clitext.UsageGroupStart,
			row("herrscher \"<texte>\"", clitext.UsageStartTask),
			row("herrscher --print \"<texte>\"", clitext.UsageStartPrint),
			row("herrscher", clitext.UsageStartTUI),
			row("herrscher version", clitext.UsageStartVersion),
			row("herrscher init", clitext.UsageStartInit),
		),
		"",
		group(clitext.UsageGroupSessions,
			row("session <create|close|list|who>", clitext.UsageSessionVerbs),
			row("agent   <create|list>", clitext.UsageAgentVerbs),
			row("host    <add|list|check|provision|rm>", clitext.UsageHostVerbs),
			row("approve <list|allow|deny|rule|mode>", clitext.UsageApproveVerbs),
			row("memory  <locate|forget|record>", clitext.UsageMemoryVerbs),
			row("models  list", clitext.UsageModelsVerbs),
		),
		"",
		group(clitext.UsageGroupDaemon,
			row("serve", clitext.UsageServe),
			row("bridge  --cmd '<command>'", clitext.UsageBridge),
			row("service <install|uninstall|…>", clitext.UsageService),
			row("commands", clitext.UsageCommands),
		),
		"",
		group(clitext.UsageGroupSetup,
			row("plugin  <list|add|remove|pin|unpin>", clitext.UsagePluginVerbs),
			row("update", clitext.UsageUpdate),
			row("install", clitext.UsageInstall),
		),
		"",
		"  " + hint.Render(tr.Text(clitext.UsageHelpFooter, nil)),
		"",
		"  " + hint.Render(tr.Text(clitext.UsageEnvDiscord, nil)),
		"  " + hint.Render(tr.Text(clitext.UsageEnvOwner, nil)),
		"  " + hint.Render(tr.Text(clitext.UsageEnvRoute, nil)),
		"  " + hint.Render(tr.Text(clitext.UsageEnvGateway, nil)),
		"  " + hint.Render(tr.Text(clitext.UsageEnvDefaultModel, nil)),
		"  " + hint.Render(tr.Text(clitext.UsageEnvInit, nil)),
	}
	if err := tr.Err(); err != nil {
		return fmt.Errorf("usage: %w", err)
	}

	fmt.Fprintln(os.Stderr, strings.Join(blocks, "\n"))
	return nil
}
