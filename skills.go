package main

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/skills"
)

func installShippedSkills(ctx context.Context, lang i18n.Language) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	dst := filepath.Join(home, ".claude", "skills")
	tr := clitext.For(lang)
	plug, notes := installPluginSkills(ctx, tr, contracts.Default.Plugins(), os.Getenv, dst)
	for _, n := range notes {
		fmt.Fprintln(os.Stderr, n)
	}
	reportSkills(tr, dst, plug)
	retired, err := retireSkills(dst, plug)
	if err != nil {
		fmt.Fprintln(os.Stderr, tr.Text(clitext.SkillsRetireFailed, clitext.Params{"Err": err.Error()}))
	}
	reportRetired(tr, dst, retired)
	return tr.Err()
}

func retireSkills(dst string, plug skills.Outcome) (skills.Outcome, error) {
	if len(plug.Partial) > 0 {
		return skills.Outcome{}, nil
	}
	return skills.Retire(dst, plug.Shipped)
}

func reportRetired(tr *clitext.Translator, dst string, out skills.Outcome) {
	for _, name := range out.Removed {
		fmt.Fprintln(os.Stderr, tr.Text(clitext.SkillRemoved, clitext.Params{"Name": name}))
	}
	for _, name := range out.Diverged {
		fmt.Fprintln(os.Stderr, tr.Text(clitext.SkillRetiredDiverged, clitext.Params{"Name": name, "Path": filepath.Join(dst, name)}))
	}
	for _, name := range out.Linked {
		fmt.Fprintln(os.Stderr, tr.Text(clitext.SkillRetiredLinked, clitext.Params{"Name": name, "Path": filepath.Join(dst, name)}))
	}
}

func reportSkills(tr *clitext.Translator, dst string, out skills.Outcome) {
	for _, name := range out.Installed {
		fmt.Fprintln(os.Stderr, tr.Text(clitext.SkillInstalled, clitext.Params{"Name": name}))
	}
	for _, name := range out.Updated {
		fmt.Fprintln(os.Stderr, tr.Text(clitext.SkillUpdated, clitext.Params{"Name": name}))
	}
	for _, name := range out.Diverged {
		fmt.Fprintln(os.Stderr, tr.Text(clitext.SkillDiverged, clitext.Params{"Name": name, "Path": filepath.Join(dst, name)}))
	}
	for _, name := range out.Linked {
		fmt.Fprintln(os.Stderr, tr.Text(clitext.SkillLinked, clitext.Params{"Name": name, "Path": filepath.Join(dst, name)}))
	}
}

func installPluginSkills(ctx context.Context, tr *clitext.Translator, plugins []contracts.Plugin, getenv func(string) string, dst string) (skills.Outcome, []string) {
	var all skills.Outcome
	var notes []string
	claimed := make(map[string]string)
	for i := range plugins {
		plugin := &plugins[i]
		if plugin.Skills == nil {
			continue
		}
		kind := plugin.Manifest.Kind
		cfg, err := pluginSkillsConfig(plugin, getenv)
		if err != nil {
			notes = append(notes, tr.Text(clitext.SkillsNotConfigured, clitext.Params{"Kind": kind, "Err": err.Error()}))
			all.Partial = append(all.Partial, kind)
			continue
		}
		tree, err := plugin.Skills(ctx, cfg)
		if err != nil {
			notes = append(notes, tr.Text(clitext.SkillsPluginFailed, clitext.Params{"Kind": kind, "Err": err.Error()}))
			all.Partial = append(all.Partial, kind)
			continue
		}
		if tree == nil {
			notes = append(notes, tr.Text(clitext.SkillsNoContent, clitext.Params{"Kind": kind}))
			all.Partial = append(all.Partial, kind)
			continue
		}
		src, dups := reserve(tr, tree, kind, claimed)
		notes = append(notes, dups...)
		if src == nil {
			continue
		}
		out, err := skills.Install(src, dst)
		if err != nil {
			notes = append(notes, tr.Text(clitext.SkillsInstallFailed, clitext.Params{"Kind": kind, "Err": err.Error()}))
			all.Partial = append(all.Partial, kind)
			continue
		}
		all.Installed = append(all.Installed, out.Installed...)
		all.Updated = append(all.Updated, out.Updated...)
		all.Diverged = append(all.Diverged, out.Diverged...)
		all.Linked = append(all.Linked, out.Linked...)
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

func reserve(tr *clitext.Translator, tree fs.FS, kind string, claimed map[string]string) (fs.FS, []string) {
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
			notes = append(notes, tr.Text(clitext.SkillDuplicate, clitext.Params{"Name": entry.Name(), "Kind": kind, "Owner": owner}))
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
