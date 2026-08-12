package main

import (
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
func installShippedSkills() {
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
	reportSkills(dst, installPluginSkills(contracts.Default.Plugins(), dst), "plugin skill")
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
// forever, for a capability that is not there.
//
// Best effort, like installShippedSkills: a daemon that cannot write there loses
// a playbook, not its ability to answer. Skills are read from a static field, so
// a gateway that never instantiates for want of a token still ships its own.
func installPluginSkills(plugins []contracts.Plugin, dst string) skills.Outcome {
	var all skills.Outcome
	for _, p := range plugins {
		if p.Skills == nil {
			continue
		}
		out, err := skills.Install(p.Skills, dst)
		if err != nil {
			fmt.Fprintln(os.Stderr, "herrscher: install skills for plugin "+p.Manifest.Kind+": "+err.Error())
			continue
		}
		all.Installed = append(all.Installed, out.Installed...)
		all.Updated = append(all.Updated, out.Updated...)
		all.Diverged = append(all.Diverged, out.Diverged...)
	}
	return all
}
