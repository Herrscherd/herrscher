package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/skills"
)

// shippedSkills are the playbooks that travel with the binary. Shipping them in
// the repo was not enough: the backend loads skills from the workspace and from
// ~/.claude/skills, neither of which is this source tree, so an agent told to
// follow "pr-job" reported the skill did not exist and improvised.
//
//go:embed skills
var shippedSkills embed.FS

// installShippedSkills materializes them where every backend looks for personal
// skills, and keeps them current: a release that rewrites a playbook must reach
// the agent, or it goes on following instructions from the day the daemon was
// first installed. Best effort on purpose: a daemon that cannot write there must
// still start — it loses a playbook, not its ability to answer.
func installShippedSkills(ctx context.Context) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	src, err := fs.Sub(shippedSkills, "skills")
	if err != nil {
		return
	}
	dst := filepath.Join(home, ".claude", "skills")
	out, err := skills.Install(src, dst)
	if err != nil {
		fmt.Fprintln(os.Stderr, "herrscher: install shipped skills: "+err.Error())
	}
	reportSkills(dst, out, "skill")
	plug, notes := installPluginSkills(ctx, contracts.Default.Plugins(), os.Getenv, dst)
	for _, n := range notes {
		fmt.Fprintln(os.Stderr, n)
	}
	reportSkills(dst, plug, "plugin skill")
}

// reportSkills says what happened. A diverged playbook is the one worth words:
// the agent is following the operator's version, so the shipped rewrite is not in
// effect, and only they can decide which one wins.
func reportSkills(dst string, out skills.Outcome, kind string) {
	for _, name := range out.Installed {
		fmt.Fprintln(os.Stderr, "herrscher: installed "+kind+" "+name)
	}
	for _, name := range out.Updated {
		fmt.Fprintln(os.Stderr, "herrscher: updated "+kind+" "+name+" to the shipped version")
	}
	for _, name := range out.Diverged {
		fmt.Fprintln(os.Stderr, "herrscher: "+kind+" "+name+" differs from the one this build ships and was left as it is; "+
			"delete "+filepath.Join(dst, name)+" to take the shipped version")
	}
}

// installPluginSkills materializes the playbooks the compiled-in plugins ship,
// into the same directory as the binary's own. A plugin's skill exists on a
// machine only if that plugin is in the build, which is the point: a Discord
// playbook on a host with no Discord gateway is noise in every agent's context,
// forever, for a capability that is not there. A plugin refines that judgement at
// runtime — its factory looks at the machine and may decline — and the second
// return says which ones did, and why, so a missing playbook is never silent.
//
// Best effort, like installShippedSkills: a daemon that cannot write there loses
// a playbook, not its ability to answer. The skills factory is called on its own
// and never through the port factory, so a gateway that never instantiates for
// want of a token still ships its own.
func installPluginSkills(ctx context.Context, plugins []contracts.Plugin, getenv func(string) string, dst string) (skills.Outcome, []string) {
	var all skills.Outcome
	var notes []string
	for _, p := range plugins {
		if p.Skills == nil {
			continue
		}
		cfg, err := pluginSkillsConfig(p, getenv)
		if err != nil {
			notes = append(notes, "herrscher: skills not configured, skipping: "+p.Manifest.Kind+": "+err.Error())
			continue
		}
		tree, err := p.Skills(ctx, cfg)
		if err != nil {
			notes = append(notes, "herrscher: no skills from plugin "+p.Manifest.Kind+": "+err.Error())
			continue
		}
		if tree == nil {
			notes = append(notes, "herrscher: no skills from plugin "+p.Manifest.Kind+": its factory handed over no playbooks")
			continue
		}
		out, err := skills.Install(tree, dst)
		if err != nil {
			notes = append(notes, "herrscher: install skills for plugin "+p.Manifest.Kind+": "+err.Error())
			continue
		}
		all.Installed = append(all.Installed, out.Installed...)
		all.Updated = append(all.Updated, out.Updated...)
		all.Diverged = append(all.Diverged, out.Diverged...)
	}
	return all, notes
}

// pluginSkillsConfig resolves what the skills factory is given. A plugin whose
// whole contribution is playbooks owns its configuration, so a required setting
// it declares is enforced and an unconfigured one is skipped and named. For any
// other category the required settings belong to the port, not to the playbook:
// enforcing them here would mean a gateway lost its skill the day its token
// expired, so what resolved is handed over and the rest is the hub's problem.
func pluginSkillsConfig(p contracts.Plugin, getenv func(string) string) (contracts.PluginConfig, error) {
	cfg, err := contracts.Resolve(p.Manifest.Config, getenv)
	if err != nil && p.Manifest.Category == contracts.CategorySkills {
		return cfg, err
	}
	return cfg, nil
}
