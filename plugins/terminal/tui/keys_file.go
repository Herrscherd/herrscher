package tui

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

const (
	keybindingsFile      = "keybindings.json"
	keybindingsSchemaURL = "https://www.schemastore.org/claude-code-keybindings.json"
	keybindingsDocsURL   = "https://code.claude.com/docs/en/keybindings"
)

type keyFileEntry struct {
	Context  string                     `json:"context"`
	Bindings map[string]json.RawMessage `json:"bindings"`
}

type keyFile struct {
	Schema   string         `json:"$schema,omitempty"`
	Docs     string         `json:"$docs,omitempty"`
	Bindings []keyFileEntry `json:"bindings"`
}

type keysSection struct {
	ImportClaude *bool    `toml:"import_claude,omitempty"`
	ChordTimeout *float64 `toml:"chord_timeout,omitempty"`
}

type keysFile struct {
	Keys keysSection `toml:"keys"`
}

type keyLoad struct {
	tr       translator
	table    *keyTable
	problems []string
	timeout  float64
}

func readKeysSection(path string) (keysSection, error) {
	var file keysFile
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return keysSection{}, nil
	}
	if _, err := toml.DecodeFile(path, &file); err != nil {
		return keysSection{}, err
	}
	return file.Keys, nil
}

func loadKeyBindings(trans translator, claudePath, herrscherPath string, section keysSection) keyLoad {
	load := keyLoad{tr: trans, table: newKeyTable()}
	for _, def := range defaultKeyBindings() {
		canon, err := canonicalChord(def.chord)
		if err != nil {
			load.problems = append(load.problems, trans.t("tui.keys.default_binding", map[string]any{"Chord": def.chord, tplArgReason: localizedReason(trans, err)}))
			continue
		}
		load.table.bind(def.ctx, canon, def.action)
	}
	if section.ImportClaude == nil || *section.ImportClaude {
		load.applyFile(claudePath, "~/.claude/"+keybindingsFile)
	}
	load.applyFile(herrscherPath, keybindingsFile)
	if section.ChordTimeout != nil {
		load.timeout = *section.ChordTimeout
	}
	return load
}

func (l *keyLoad) applyFile(path, label string) {
	if path == "" {
		return
	}
	raw, err := os.ReadFile(filepath.Clean(path))
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		l.problems = append(l.problems, l.tr.t("tui.keys.file_ignored", map[string]any{tplArgLabel: label, tplArgReason: localizedReason(l.tr, err)}))
		return
	}
	var file keyFile
	if err := json.Unmarshal(raw, &file); err != nil {
		l.problems = append(l.problems, l.tr.t("tui.keys.file_ignored", map[string]any{tplArgLabel: label, tplArgReason: localizedReason(l.tr, err)}))
		return
	}
	seen := make(map[keyContext]map[string]bool)
	for _, entry := range file.Bindings {
		l.applyEntry(entry, label, seen)
	}
}

func (l *keyLoad) applyEntry(entry keyFileEntry, label string, seen map[keyContext]map[string]bool) {
	ctx := keyContext(entry.Context)
	if !isKnownKeyContext(ctx) {
		l.problems = append(l.problems, l.tr.t("tui.keys.unknown_context", map[string]any{tplArgLabel: label, tplArgReason: localizedReason(l.tr, errKeyUnknownCtx), "Context": entry.Context}))
		return
	}
	if seen[ctx] == nil {
		seen[ctx] = make(map[string]bool)
	}
	chords := make([]string, 0, len(entry.Bindings))
	for chord := range entry.Bindings {
		chords = append(chords, chord)
	}
	sort.Strings(chords)
	for _, spec := range chords {
		canon, err := l.checkChord(spec, label)
		if err != nil {
			continue
		}
		if seen[ctx][canon] {
			l.problems = append(l.problems, l.tr.t("tui.keys.duplicate", map[string]any{tplArgLabel: label, tplArgReason: localizedReason(l.tr, errKeyDuplicate), tplArgSpec: spec}))
			continue
		}
		seen[ctx][canon] = true
		l.applyValue(ctx, canon, spec, entry.Bindings[spec], label)
	}
}

func (l *keyLoad) checkChord(spec, label string) (string, error) {
	canon, err := canonicalChord(spec)
	if err == nil {
		err = reliableChord(canon)
	}
	if err != nil {
		l.problems = append(l.problems, l.tr.t("tui.keys.binding_failed", map[string]any{tplArgLabel: label, tplArgSpec: spec, tplArgReason: localizedReason(l.tr, err)}))
		return "", err
	}
	return canon, nil
}

func (l *keyLoad) applyValue(ctx keyContext, canon, spec string, value json.RawMessage, label string) {
	if string(value) == "null" {
		l.table.unbind(ctx, canon)
		return
	}
	var action string
	if err := json.Unmarshal(value, &action); err != nil {
		l.problems = append(l.problems, l.tr.t("tui.keys.binding_failed", map[string]any{tplArgLabel: label, tplArgSpec: spec, tplArgReason: localizedReason(l.tr, errKeyBadValue)}))
		return
	}
	if !isKnownKeyAction(keyAction(action)) {
		l.problems = append(l.problems, l.tr.t("tui.keys.unknown_action", map[string]any{tplArgLabel: label, tplArgSpec: spec, tplArgReason: localizedReason(l.tr, errKeyUnknownAction), tplArgAction: action}))
		return
	}
	l.table.bind(ctx, canon, keyAction(action))
}

func isKnownKeyContext(ctx keyContext) bool {
	for _, known := range knownKeyContexts() {
		if known == ctx {
			return true
		}
	}
	return slices.Contains(claudeReferenceContexts(), ctx)
}

func keybindingsSkeleton() []byte {
	file := keyFile{Schema: keybindingsSchemaURL, Docs: keybindingsDocsURL, Bindings: []keyFileEntry{}}
	out, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return nil
	}
	return append(out, '\n')
}

func joinProblems(problems []string) string {
	return strings.Join(problems, " ; ")
}
