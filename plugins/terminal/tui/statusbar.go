package tui

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const defaultMode = "Manuel"

const (
	rankBusy = iota
	rankExtra
	rankBackground
	rankWorktree
	rankModel
	rankCost
	rankQueue
	rankDetail
	rankGauge
	rankRepo
	rankNever = math.MaxInt
)

const (
	barModeID     = "mode"
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

var barFieldSpecs = []barFieldSpec{
	{id: barModeID, label: "mode et", note: "Manuel, Édits acceptés, Plan, Auto en rouge", defaultOn: true},
	{id: "vendor", label: "vendeur et modèle", note: "claude, codex, cursor", defaultOn: true},
	{id: "repo", label: "dépôt et branche", note: "herrscher:feat/tui-v2", defaultOn: true},
	{id: "wt", label: "worktree", note: "(worktree) si à elle", defaultOn: true},
	{id: "ctx", label: "jauge de contexte", note: "█ ░, or 80 %, rouge 95 %", defaultOn: true},
	{id: "detail", label: "tokens du contexte", note: "82k/200k", defaultOn: true},
	{id: "cost", label: "coût", note: "facturation réelle", defaultOn: true},
	{id: "queue", label: "file en attente", note: "○ file, ↪ réorientations", defaultOn: true},
	{id: "bg", label: "tâches de fond", note: "go test -race en fond", defaultOn: true},
	{id: "dur", label: "durée du tour", note: "1m12s", defaultOn: false},
	{id: "quota", label: "quotas 5 h et 7 j", note: "abonnement Claude", defaultOn: false},
}

func (s barFieldSpec) labelWith(glyphs glyphSet) string {
	if s.id == barModeID {
		return s.label + " " + glyphs.shiftTab
	}
	return s.label
}

func barFieldSpecOf(id string) barFieldSpec {
	for _, spec := range barFieldSpecs {
		if spec.id == id {
			return spec
		}
	}
	return barFieldSpec{id: id, label: id}
}

func defaultBarOrder() []string {
	ids := make([]string, 0, len(barFieldSpecs))
	for _, spec := range barFieldSpecs {
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
	mode       string
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
	return barState{mode: defaultMode, config: defaultStatuslineConfig()}
}

func (b *barState) setMode(mode string) { b.mode = mode }

func (b *barState) setQueued(count int) { b.queued = count }

func (b *barState) setBackground(count int) { b.background = count }

func (b *barState) setQuota(text string) { b.quota = text }

func (b *barState) modeName() string {
	if b.mode == "" {
		return defaultMode
	}
	return b.mode
}

func (b *barState) fieldIDs() []string {
	if len(b.order) > 0 {
		return b.order
	}
	return defaultBarOrder()
}

func (b *barState) isShown(id string) bool {
	if shown, ok := b.toggled[id]; ok {
		return shown
	}
	return barFieldSpecOf(id).defaultOn
}

func (b *barState) toggle(id string) {
	if b.toggled == nil {
		b.toggled = make(map[string]bool)
	}
	b.toggled[id] = !b.isShown(id)
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

func modeChipStyle(mode string, styles uiStyles) lipgloss.Style {
	var role lipgloss.Style
	switch mode {
	case "Auto":
		role = styles.red
	case "Plan":
		role = styles.violet
	case "Édits acceptés":
		role = styles.accent
	default:
		role = styles.text
	}
	return role.Bold(true).Reverse(true)
}

func (m *model) statusBar(tb *tab, width int) string {
	if width < 1 {
		return ""
	}
	if m.bar.scripted() && m.bar.script.line != "" {
		return truncate(m.bar.script.line, width)
	}
	if m.bar.scripted() && m.bar.script.failed {
		return m.builtinBar(tb, width-barFailTail) + " " + m.st.dim.Render(m.st.glyphs.failure)
	}
	return m.builtinBar(tb, width)
}

func (m *model) builtinBar(tb *tab, width int) string {
	if width < 1 {
		return ""
	}
	parts := m.barParts(tb)
	for {
		line := m.composeBar(parts)
		if lipgloss.Width(line) <= width {
			return line
		}
		next, dropped := dropLowestLayer(parts)
		if !dropped {
			return truncate(line, width)
		}
		parts = next
	}
}

func (m *model) barParts(tb *tab) []barPart {
	info, _ := m.activeInfo()
	used := m.contextTokens(tb)
	var parts []barPart
	if m.bar.isShown(barModeID) {
		parts = append(parts, m.modePart())
	}
	for _, id := range m.bar.fieldIDs() {
		if id == barModeID {
			continue
		}
		if part, ok := m.fieldPart(id, tb, info, used); ok {
			parts = append(parts, part)
		}
	}
	if tb.busy {
		parts = append(parts, single(m.st.spinner.Render(m.spinnerHint(tb)), rankBusy))
	}
	if mouse := m.mouseStatus(); mouse != "" {
		parts = append(parts, single(mouse, rankExtra))
	}
	return parts
}

func (m *model) modePart() barPart {
	mode := m.bar.modeName()
	chip := modeChipStyle(mode, m.st).Render(" " + mode + " ")
	return single(chip+m.st.dim.Render(" "+m.st.glyphs.shiftTab), rankNever)
}

func (m *model) fieldPart(id string, tb *tab, info contracts.SessionInfo, used int) (barPart, bool) {
	if !m.bar.isShown(id) {
		return barPart{}, false
	}
	switch id {
	case "vendor":
		return m.vendorPart(info)
	case "repo":
		return m.repoPart(info)
	case "ctx":
		return m.contextPart(info, used)
	case "cost":
		if tb.costTotal > 0 {
			return single(m.st.green.Render(formatCost(tb.costTotal)), rankCost), true
		}
	case "queue":
		if m.bar.queued > 0 {
			return single(m.st.text.Render(fmt.Sprintf("%d en file", m.bar.queued)), rankQueue), true
		}
	case "bg":
		if m.bar.background > 0 {
			return single(m.st.dim.Render(m.spinFrame()+fmt.Sprintf(" %d en fond", m.bar.background)), rankBackground), true
		}
	case "dur":
		if tb.busy && !tb.startedAt.IsZero() {
			return single(m.st.dim.Render("turn ")+m.st.text.Render(formatDuration(time.Since(tb.startedAt))), rankExtra), true
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
		{text: contextGauge(ratio, contextCells, m.st) + " ", rank: rankGauge},
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
	at, layerAt := -1, -1
	for i, part := range parts {
		for j, layer := range part.layers {
			if layer.rank == rankNever {
				continue
			}
			if at < 0 || layer.rank < parts[at].layers[layerAt].rank {
				at, layerAt = i, j
			}
		}
	}
	if at < 0 {
		return parts, false
	}
	remaining := make([]barLayer, 0, len(parts[at].layers)-1)
	remaining = append(remaining, parts[at].layers[:layerAt]...)
	remaining = append(remaining, parts[at].layers[layerAt+1:]...)
	out := append([]barPart(nil), parts...)
	if len(remaining) == 0 {
		return append(out[:at], out[at+1:]...), true
	}
	out[at] = barPart{layers: remaining}
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
		gitDir = linkedGitDir(dir, dotGit)
		if gitDir == "" {
			return "", linked
		}
	}
	head, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return "", linked
	}
	branch, ok := strings.CutPrefix(strings.TrimSpace(string(head)), headRefPrefix)
	if !ok {
		return "", linked
	}
	return branch, linked
}

func linkedGitDir(dir, dotGit string) string {
	data, err := os.ReadFile(dotGit)
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
