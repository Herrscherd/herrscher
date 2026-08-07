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
// skills, once. Best effort on purpose: a daemon that cannot write there must
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
	made, err := skills.Install(src, filepath.Join(home, ".claude", "skills"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "herrscher: install shipped skills: "+err.Error())
	}
	for _, name := range made {
		fmt.Fprintln(os.Stderr, "herrscher: installed skill "+name)
	}

	for _, name := range installPluginSkills(contracts.Default.Plugins(), filepath.Join(home, ".claude", "skills")) {
		fmt.Fprintln(os.Stderr, "herrscher: installed plugin skill "+name)
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
func installPluginSkills(plugins []contracts.Plugin, dst string) []string {
	var made []string
	for _, p := range plugins {
		if p.Skills == nil {
			continue
		}
		names, err := skills.Install(p.Skills, dst)
		if err != nil {
			fmt.Fprintln(os.Stderr, "herrscher: install skills for plugin "+p.Manifest.Kind+": "+err.Error())
			continue
		}
		made = append(made, names...)
	}
	return made
}
