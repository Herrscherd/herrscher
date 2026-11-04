package main

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/skills"
)

func installShippedSkills(ctx context.Context) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	dst := filepath.Join(home, ".claude", "skills")
	plug, notes := installPluginSkills(ctx, contracts.Default.Plugins(), os.Getenv, dst)
	for _, n := range notes {
		fmt.Fprintln(os.Stderr, n)
	}
	reportSkills(dst, plug, "plugin skill")
	retired, err := retireSkills(dst, plug)
	if err != nil {
		fmt.Fprintln(os.Stderr, "herrscher: retire skills no longer shipped: "+err.Error())
	}
	reportRetired(dst, retired)
}

func retireSkills(dst string, plug skills.Outcome) (skills.Outcome, error) {
	if len(plug.Partial) > 0 {
		return skills.Outcome{}, nil
	}
	return skills.Retire(dst, plug.Shipped)
}

func reportRetired(dst string, out skills.Outcome) {
	for _, name := range out.Removed {
		fmt.Fprintln(os.Stderr, "herrscher: removed skill "+name+": no build ships it any more")
	}
	for _, name := range out.Diverged {
		fmt.Fprintln(os.Stderr, "herrscher: skill "+name+" is no longer shipped and was edited here; it was left as it is, "+
			"delete "+filepath.Join(dst, name)+" to remove it")
	}
}

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

func installPluginSkills(ctx context.Context, plugins []contracts.Plugin, getenv func(string) string, dst string) (skills.Outcome, []string) {
	var all skills.Outcome
	var notes []string
	claimed := make(map[string]string)
	for i := range plugins {
		plugin := &plugins[i]
		if plugin.Skills == nil {
			continue
		}
		cfg, err := pluginSkillsConfig(plugin, getenv)
		if err != nil {
			notes = append(notes, "herrscher: skills not configured, skipping: "+plugin.Manifest.Kind+": "+err.Error())
			all.Partial = append(all.Partial, plugin.Manifest.Kind)
			continue
		}
		tree, err := plugin.Skills(ctx, cfg)
		if err != nil {
			notes = append(notes, "herrscher: no skills from plugin "+plugin.Manifest.Kind+": "+err.Error())
			all.Partial = append(all.Partial, plugin.Manifest.Kind)
			continue
		}
		if tree == nil {
			notes = append(notes, "herrscher: no skills from plugin "+plugin.Manifest.Kind+": its factory handed over no playbooks")
			all.Partial = append(all.Partial, plugin.Manifest.Kind)
			continue
		}
		src, dups := reserve(tree, plugin.Manifest.Kind, claimed)
		notes = append(notes, dups...)
		if src == nil {
			continue
		}
		out, err := skills.Install(src, dst)
		if err != nil {
			notes = append(notes, "herrscher: install skills for plugin "+plugin.Manifest.Kind+": "+err.Error())
			all.Partial = append(all.Partial, plugin.Manifest.Kind)
			continue
		}
		all.Installed = append(all.Installed, out.Installed...)
		all.Updated = append(all.Updated, out.Updated...)
		all.Diverged = append(all.Diverged, out.Diverged...)
		all.Shipped = append(all.Shipped, out.Shipped...)
	}
	return all, notes
}

func pluginSkillsConfig(plugin *contracts.Plugin, getenv func(string) string) (contracts.PluginConfig, error) {
	cfg, err := contracts.Resolve(plugin.Manifest.Config, getenv)
	if err != nil && plugin.Manifest.Category == contracts.CategorySkills {
		return cfg, err
	}
	return cfg, nil
}

func reserve(tree fs.FS, kind string, claimed map[string]string) (fs.FS, []string) {
	entries, err := fs.ReadDir(tree, ".")
	if err != nil {
		return tree, nil
	}
	var notes []string
	hidden := make(map[string]bool)
	kept := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if owner, taken := claimed[entry.Name()]; taken {
			hidden[entry.Name()] = true
			notes = append(notes, "herrscher: skill "+entry.Name()+" from plugin "+kind+
				" was left out: plugin "+owner+" already ships one by that name")
			continue
		}
		claimed[entry.Name()] = kind
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
	for _, entry := range entries {
		if !w.hidden[entry.Name()] {
			out = append(out, entry)
		}
	}
	return out, nil
}
