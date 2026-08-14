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
// machine only if that plugin is in the build, and its factory refines that at
// runtime by looking at the machine and declining; the second return says which
// ones did, and why, so a missing playbook is never silent.
//
// Best effort, like installShippedSkills: a daemon that cannot write there loses
// a playbook, not its ability to answer.
func installPluginSkills(ctx context.Context, plugins []contracts.Plugin, getenv func(string) string, dst string) (skills.Outcome, []string) {
	var all skills.Outcome
	var notes []string
	claimed := map[string]string{}
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
		src, dups := reserve(tree, p.Manifest.Kind, claimed)
		notes = append(notes, dups...)
		if src == nil {
			continue
		}
		out, err := skills.Install(src, dst)
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

// reserve records under kind the skill directories tree carries, and hands back
// the tree with the ones another plugin already claimed hidden — nil when that
// leaves nothing. Two plugins shipping the same skill name otherwise take turns
// overwriting it: each start rewrites the directory and announces an update
// nobody asked for, and whichever plugin registered first loses its playbook in
// silence. The host imposes its kind as a prefix on contributed commands for the
// same reason; a skill name is read by the agent, so it cannot be prefixed, and
// naming the clash is what is left.
func reserve(tree fs.FS, kind string, claimed map[string]string) (fs.FS, []string) {
	entries, err := fs.ReadDir(tree, ".")
	if err != nil {
		return tree, nil // not ours to report: skills.Install says it better.
	}
	var notes []string
	hidden := map[string]bool{}
	kept := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if owner, taken := claimed[e.Name()]; taken {
			hidden[e.Name()] = true
			notes = append(notes, "herrscher: skill "+e.Name()+" from plugin "+kind+
				" was left out: plugin "+owner+" already ships one by that name")
			continue
		}
		claimed[e.Name()] = kind
		kept++
	}
	switch {
	case len(hidden) == 0:
		return tree, nil
	case kept == 0:
		return nil, notes
	}
	return without{FS: tree, hidden: hidden}, notes
}

// without is a tree with some of its top-level directories taken out. Only the
// root is filtered; everything below it is the wrapped tree's own business.
type without struct {
	fs.FS
	hidden map[string]bool
}

func (w without) ReadDir(name string) ([]fs.DirEntry, error) {
	entries, err := fs.ReadDir(w.FS, name)
	if name != "." || err != nil {
		return entries, err
	}
	out := make([]fs.DirEntry, 0, len(entries))
	for _, e := range entries {
		if !w.hidden[e.Name()] {
			out = append(out, e)
		}
	}
	return out, nil
}
