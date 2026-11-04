package tui

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const (
	rankDetail = iota
	rankWorktree
	rankBusy
	rankExtra
	rankBackground
	rankModel
	rankCost
	rankQueue
	rankGauge
	rankRepo
	rankNever = math.MaxInt
)

const (
	barModeID     = "mode"
	fieldVendor   = "vendor"
	fieldQueue    = "queue"
	barFailTail   = 2
	gitDirPrefix  = "gitdir: "
	headRefPrefix = "ref: refs/heads/"
)

type barFieldSpec struct {
	id        string
	label     string
	note      string
	defaultOn bool
}

func barFieldSpecs() []barFieldSpec {
	return []barFieldSpec{
		{id: barModeID, label: "mode et", note: "Manuel, Édits acceptés, Plan, Auto en rouge", defaultOn: true},
		{id: fieldVendor, label: "vendeur et modèle", note: "claude, codex, cursor", defaultOn: true},
		{id: "repo", label: "dépôt et branche", note: "herrscher:feat/tui-v2", defaultOn: true},
		{id: "wt", label: "worktree", note: "(worktree) si à elle", defaultOn: true},
		{id: "ctx", label: "jauge de contexte", note: "█ ░, or 80 %, rouge 95 %", defaultOn: true},
		{id: "detail", label: "tokens du contexte", note: "82k/200k", defaultOn: true},
		{id: "cost", label: "coût", note: "facturation réelle", defaultOn: true},
		{id: fieldQueue, label: "file en attente", note: "○ file, ↪ réorientations", defaultOn: true},
		{id: "bg", label: "tâches de fond", note: "go test -race en fond", defaultOn: true},
		{id: "dur", label: "durée du tour", note: "1m12s", defaultOn: false},
		{id: "quota", label: "quotas 5 h et 7 j", note: "abonnement Claude", defaultOn: false},
	}
}

func (s barFieldSpec) labelWith(glyphs glyphSet) string {
	if s.id == barModeID {
		return s.label + " " + glyphs.shiftTab
	}
	return s.label
}

func barFieldSpecOf(fieldID string) barFieldSpec {
	for _, spec := range barFieldSpecs() {
		if spec.id == fieldID {
			return spec
		}
	}
	return barFieldSpec{id: fieldID, label: fieldID}
}

func defaultBarOrder() []string {
	ids := make([]string, 0, len(barFieldSpecs()))
	for _, spec := range barFieldSpecs() {
		ids = append(ids, spec.id)
	}
	return ids
}

type scriptRun struct {
	busy   bool
	armed  bool
	failed bool
	line   string
}

type gitHead struct {
	dir    string
	branch string
	linked bool
	at     time.Time
}

type barState struct {
	queued     int
	background int
	quota      string
	order      []string
	toggled    map[string]bool
	config     statuslineConfig
	cursor     int
	open       bool
	script     scriptRun
	git        gitHead
	file       string
}

func newBarState() barState {
	return barState{config: defaultStatuslineConfig()}
}

func (b *barState) setQueued(count int) { b.queued = count }

func (b *barState) setBackground(count int) { b.background = count }

func (b *barState) fieldIDs() []string {
	if len(b.order) > 0 {
		return b.order
	}
	return defaultBarOrder()
}

func (b *barState) isShown(fieldID string) bool {
	if shown, ok := b.toggled[fieldID]; ok {
		return shown
	}
	return barFieldSpecOf(fieldID).defaultOn
}

func (b *barState) toggle(fieldID string) {
	if b.toggled == nil {
		b.toggled = make(map[string]bool)
	}
	b.toggled[fieldID] = !b.isShown(fieldID)
}

func (b *barState) moveField(index, delta int) bool {
	ids := append([]string(nil), b.fieldIDs()...)
	target := index + delta
	if index < 0 || index >= len(ids) || target < 0 || target >= len(ids) {
		return false
	}
	if ids[index] == barModeID || ids[target] == barModeID {
		return false
	}
	ids[index], ids[target] = ids[target], ids[index]
	b.order = ids
	return true
}

func (b *barState) scripted() bool { return b.config.Source == barSourceScript }

type barLayer struct {
	text string
	rank int
}

type barPart struct {
	layers []barLayer
}

func single(text string, rank int) barPart {
	return barPart{layers: []barLayer{{text: text, rank: rank}}}
}

func (m *model) statusBar(target *tab, width int) string {
	if width < 1 {
		return ""
	}
	if m.bar.scripted() && m.bar.script.line != "" {
		return cutText(m.bar.script.line, width)
	}
	if m.bar.scripted() && m.bar.script.failed {
		return m.builtinBar(target, width-barFailTail) + " " + m.st.dim.Render(m.st.glyphs.failure)
	}
	return m.builtinBar(target, width)
}

func (m *model) builtinBar(target *tab, width int) string {
	if width < 1 {
		return ""
	}
	parts := m.barParts(target)
	for {
		line := m.composeBar(parts)
		if lipgloss.Width(line) <= width {
			return line
		}
		next, dropped := dropLowestLayer(parts)
		if !dropped {
			return cutText(line, width)
		}
		parts = next
	}
}

func (m *model) barParts(target *tab) []barPart {
	info, _ := m.activeInfo()
	used := m.contextTokens(target)
	var parts []barPart
	if m.bar.isShown(barModeID) {
		parts = append(parts, single(m.modeChip(target), rankNever))
	}
	for _, fieldID := range m.bar.fieldIDs() {
		if fieldID == barModeID {
			continue
		}
		if part, ok := m.fieldPart(fieldID, target, info, used); ok {
			parts = append(parts, part)
		}
	}
	if target.busy {
		parts = append(parts, single(m.st.spinner.Render(m.spinnerHint(target)), rankBusy))
	}
	if mouse := m.mouseStatus(); mouse != "" {
		parts = append(parts, single(mouse, rankExtra))
	}
	return parts
}

func costLabel(total float64) string {
	if total <= 0 {
		return "$0.00"
	}
	return formatCost(total)
}

func (m *model) fieldPart(fieldID string, target *tab, info contracts.SessionInfo, used int) (barPart, bool) {
	if !m.bar.isShown(fieldID) {
		return barPart{}, false
	}
	switch fieldID {
	case fieldVendor:
		return m.vendorPart(info)
	case "repo":
		return m.repoPart(info)
	case "ctx":
		return m.contextPart(info, used)
	case "cost":
		return single(m.st.green.Render(costLabel(target.costTotal)), rankCost), true
	case fieldQueue:
		if m.bar.queued > 0 {
			return single(m.st.text.Render(fmt.Sprintf("%d en file", m.bar.queued)), rankQueue), true
		}
	case "bg":
		if m.bar.background > 0 {
			return single(m.st.dim.Render(m.spinFrame()+fmt.Sprintf(" %d en fond", m.bar.background)), rankBackground), true
		}
	case "dur":
		if target.busy && !target.startedAt.IsZero() {
			return single(m.st.dim.Render("turn ")+m.st.text.Render(formatDuration(time.Since(target.startedAt))), rankExtra), true
		}
	case "quota":
		if m.bar.quota != "" {
			return single(m.st.dim.Render(m.bar.quota), rankExtra), true
		}
	}
	return barPart{}, false
}

func (m *model) vendorPart(info contracts.SessionInfo) (barPart, bool) {
	if info.Vendor == "" {
		return barPart{}, false
	}
	part := barPart{layers: []barLayer{{text: m.st.text.Bold(true).Render(info.Vendor), rank: rankNever}}}
	if label := modelLabel(info); label != "" {
		part.layers = append(part.layers, barLayer{text: " " + m.st.dim.Render(label), rank: rankModel})
	}
	return part, true
}

func (m *model) repoPart(info contracts.SessionInfo) (barPart, bool) {
	branch, linked := m.gitHeadOf(info.Dir)
	label := info.Project
	if branch != "" {
		if label != "" {
			label += ":"
		}
		label += branch
	}
	if label == "" {
		return barPart{}, false
	}
	part := barPart{layers: []barLayer{{text: m.st.text.Render(label), rank: rankRepo}}}
	if linked && m.bar.isShown("wt") {
		part.layers = append(part.layers, barLayer{text: " " + m.st.dim.Render("(worktree)"), rank: rankWorktree})
	}
	return part, true
}

func (m *model) contextPart(info contracts.SessionInfo, used int) (barPart, bool) {
	if used <= 0 {
		return barPart{}, false
	}
	limit := contextLimit(info.Vendor)
	ratio := contextRatio(used, limit)
	percent := palierStyle(ratio, m.st).Render(contextPercent(used, limit))
	part := barPart{layers: []barLayer{
		{text: m.st.dim.Render("ctx "), rank: rankNever},
		{text: contextGauge(ratio, m.st) + " ", rank: rankGauge},
		{text: percent, rank: rankNever},
	}}
	if m.bar.isShown("detail") {
		part.layers = append(part.layers, barLayer{text: " " + m.st.dim.Render(formatContext(used, limit)), rank: rankDetail})
	}
	return part, true
}

func (m *model) composeBar(parts []barPart) string {
	texts := make([]string, 0, len(parts))
	for _, part := range parts {
		var text strings.Builder
		for _, layer := range part.layers {
			text.WriteString(layer.text)
		}
		texts = append(texts, text.String())
	}
	return strings.Join(texts, m.st.sep)
}

func dropLowestLayer(parts []barPart) ([]barPart, bool) {
	dropPart, dropLayer := -1, -1
	for partIndex, part := range parts {
		for layerIndex, layer := range part.layers {
			if layer.rank == rankNever {
				continue
			}
			if dropPart < 0 || layer.rank < parts[dropPart].layers[dropLayer].rank {
				dropPart, dropLayer = partIndex, layerIndex
			}
		}
	}
	if dropPart < 0 {
		return parts, false
	}
	remaining := make([]barLayer, 0, len(parts[dropPart].layers)-1)
	remaining = append(remaining, parts[dropPart].layers[:dropLayer]...)
	remaining = append(remaining, parts[dropPart].layers[dropLayer+1:]...)
	out := append([]barPart(nil), parts...)
	if len(remaining) == 0 {
		return append(out[:dropPart], out[dropPart+1:]...), true
	}
	out[dropPart] = barPart{layers: remaining}
	return out, true
}

func (m *model) gitHeadOf(dir string) (string, bool) {
	if dir == "" {
		return "", false
	}
	cached := &m.bar.git
	if cached.dir != dir || time.Since(cached.at) >= infoTTL {
		branch, linked := readGitHead(dir)
		*cached = gitHead{dir: dir, branch: branch, linked: linked, at: time.Now()}
	}
	return cached.branch, cached.linked
}

func readGitHead(dir string) (string, bool) {
	dotGit := filepath.Join(dir, ".git")
	stat, err := os.Stat(dotGit)
	if err != nil {
		return "", false
	}
	gitDir, linked := dotGit, false
	if !stat.IsDir() {
		linked = true
		gitDir = linkedGitDir(dir)
		if gitDir == "" {
			return "", linked
		}
	}
	head, err := readInRoot(gitDir, "HEAD")
	if err != nil {
		return "", linked
	}
	branch, ok := strings.CutPrefix(strings.TrimSpace(string(head)), headRefPrefix)
	if !ok {
		return "", linked
	}
	return branch, linked
}

func linkedGitDir(dir string) string {
	data, err := readInRoot(dir, ".git")
	if err != nil {
		return ""
	}
	target, ok := strings.CutPrefix(strings.TrimSpace(string(data)), gitDirPrefix)
	if !ok {
		return ""
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(dir, target)
	}
	return target
}

func readInRoot(dir, name string) ([]byte, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	data, readErr := root.ReadFile(name)
	closeErr := root.Close()
	return data, errors.Join(readErr, closeErr)
}
