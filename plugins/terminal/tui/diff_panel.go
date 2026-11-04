package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	dlgNote   = "note"
	dlgCommit = "commit"

	diffWideWidth    = 100
	diffListWidth    = 42
	diffNumWidth     = 4
	diffTwoNumsWidth = 70
	diffStackedFiles = 6
	diffPageRows     = 10
)

type diffPanel struct {
	open      bool
	review    bool
	channel   string
	session   string
	scope     string
	startedAt time.Time
	gen       int
	loading   bool
	report    diffReport
	err       error
	sel       int
	scroll    int
	reviewed  map[string]bool
	dlg       string
	field     string
	editing   bool
	message   string
	busy      bool
	notice    string
	noticeBad bool
}

type diffLoadedMsg struct {
	gen    int
	report diffReport
	err    error
}

type diffActionMsg struct {
	notice string
	bad    bool
}

type turnStartMsg struct {
	channel string
	tree    string
}

type queuedTurn struct {
	text string
	atts []Attachment
}

func (m *model) diffRepo(channel string) gitRepo {
	return newGitRepo(m.sessionDir(channel), m.diffRun)
}

func (m *model) sessionDir(channel string) string {
	if name := m.sessionName(channel); name != "" {
		sessions := m.tm.Sessions()
		for i := range sessions {
			if sessions[i].Name == name && sessions[i].Dir != "" {
				return sessions[i].Dir
			}
		}
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}

func (m *model) turnStartCmd(channel string) tea.Cmd {
	repo := m.diffRepo(channel)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), gitCallLimit)
		defer cancel()
		tree, _ := repo.snapshot(ctx)
		return turnStartMsg{channel: channel, tree: tree}
	}
}

func (m *model) openDiff(review bool) tea.Cmd {
	tb := m.tabs[m.active]
	if tb == nil {
		m.flash = "no session open"
		return nil
	}
	p := &m.diff
	*p = diffPanel{
		open:      true,
		review:    review,
		channel:   m.active,
		session:   m.sessionName(m.active),
		scope:     diffScopeTurn,
		startedAt: tb.startedAt,
		gen:       p.gen,
		reviewed:  map[string]bool{},
	}
	return m.loadDiffCmd()
}

func (m *model) loadDiffCmd() tea.Cmd {
	p := &m.diff
	p.gen++
	p.loading = true
	gen, scope, repo := p.gen, p.scope, m.diffRepo(p.channel)
	turnStart := ""
	if tb := m.tabs[p.channel]; tb != nil {
		turnStart = tb.turnStart
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), diffLoadLimit)
		defer cancel()
		report, err := repo.loadDiff(ctx, scope, turnStart)
		return diffLoadedMsg{gen: gen, report: report, err: err}
	}
}

func (m *model) handleDiffMsg(msg tea.Msg) (tea.Cmd, bool) {
	p := &m.diff
	switch msg := msg.(type) {
	case diffLoadedMsg:
		if msg.gen == p.gen && p.open {
			p.loading = false
			p.err = msg.err
			p.report = msg.report
			p.clampSel()
		}
		return nil, true
	case diffActionMsg:
		p.busy = false
		p.notice, p.noticeBad = msg.notice, msg.bad
		if p.open {
			return m.loadDiffCmd(), true
		}
		return nil, true
	case turnStartMsg:
		if tb := m.tabs[msg.channel]; tb != nil {
			tb.turnStart = msg.tree
			for _, turn := range tb.queued {
				m.tm.Submit(msg.channel, turn.text, turn.atts)
			}
			tb.queued, tb.snapshotting = nil, false
		}
		return nil, true
	}
	return nil, false
}

func (m *model) handleDiffKey(msg tea.KeyMsg) tea.Cmd {
	switch m.diff.dlg {
	case dlgNote:
		return m.handleDiffNoteKey(msg)
	case dlgCommit:
		return m.handleDiffCommitKey(msg)
	}
	return m.handleDiffPanelKey(msg)
}

func (m *model) handleDiffPanelKey(msg tea.KeyMsg) tea.Cmd {
	p := &m.diff
	switch msg.String() {
	case "esc", "ctrl+d":
		p.open = false
	case "ctrl+c":
		cmd, _ := m.interruptOrQuit()
		return cmd
	case "up":
		p.moveSel(-1)
	case "down":
		p.moveSel(1)
	case "pgup":
		p.scrollBy(-diffPageRows)
	case "pgdown":
		p.scrollBy(diffPageRows)
	case " ":
		p.toggleReviewed()
	case "enter":
		return m.editDiffFile()
	case "i":
		if _, ok := p.selected(); ok {
			p.dlg, p.field = dlgNote, ""
		}
	case "c":
		p.dlg, p.message, p.editing = dlgCommit, draftCommitMessage(p.report.Files), false
	case "p":
		p.message = draftCommitMessage(p.report.Files)
		return m.commitCmd(true)
	case "b":
		p.toggleScope()
		return m.loadDiffCmd()
	}
	return nil
}

func (m *model) handleDiffNoteKey(msg tea.KeyMsg) tea.Cmd {
	p := &m.diff
	switch msg.String() {
	case "esc":
		p.dlg = ""
	case "enter":
		return m.sendDiffNote()
	case "backspace":
		p.field = dropLastRune(p.field)
	default:
		if typed, ok := typedText(msg); ok {
			p.field += typed
		}
	}
	return nil
}

func (m *model) sendDiffNote() tea.Cmd {
	p := &m.diff
	file, ok := p.selected()
	text := strings.TrimSpace(p.field)
	if !ok || text == "" {
		return nil
	}
	p.dlg, p.field = "", ""
	p.notice, p.noticeBad = "consigne envoyée sur "+file.Path, false
	return m.submitTo(p.channel, file.Path+": "+text, nil)
}

func (m *model) handleDiffCommitKey(msg tea.KeyMsg) tea.Cmd {
	p := &m.diff
	if p.editing {
		return m.handleDiffMessageEdit(msg)
	}
	switch msg.String() {
	case "esc":
		p.dlg = ""
	case "enter":
		return m.commitCmd(false)
	case "p":
		return m.commitCmd(true)
	case "e":
		p.editing = true
	}
	return nil
}

func (m *model) handleDiffMessageEdit(msg tea.KeyMsg) tea.Cmd {
	p := &m.diff
	switch msg.String() {
	case "esc":
		p.editing = false
	case "enter":
		return m.commitCmd(false)
	case "ctrl+j":
		p.message += "\n"
	case "backspace":
		p.message = dropLastRune(p.message)
	default:
		if typed, ok := typedText(msg); ok {
			p.message += typed
		}
	}
	return nil
}

func (m *model) commitCmd(withPR bool) tea.Cmd {
	p := &m.diff
	message := strings.TrimSpace(p.message)
	if p.busy {
		return nil
	}
	if message == "" {
		p.dlg = ""
		p.notice, p.noticeBad = "rien à commiter dans cette portée", true
		return nil
	}
	p.busy, p.dlg, p.editing = true, "", false
	subject, body := splitMessage(message)
	repo, base := m.diffRepo(p.channel), p.report.Base
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), prCallLimit+gitCallLimit)
		defer cancel()
		sha, err := repo.commit(ctx, message)
		if err != nil {
			return diffActionMsg{notice: "commit échoué : " + err.Error(), bad: true}
		}
		if !withPR {
			return diffActionMsg{notice: "commit " + sha + " " + subject}
		}
		number, err := repo.openPR(ctx, base, subject, body)
		if err != nil {
			return diffActionMsg{notice: "commit " + sha + ", PR échouée : " + err.Error(), bad: true}
		}
		return diffActionMsg{notice: fmt.Sprintf("commit %s, PR #%d ouverte contre %s", sha, number, base)}
	}
}

func splitMessage(message string) (string, string) {
	subject, body, _ := strings.Cut(message, "\n")
	return strings.TrimSpace(subject), strings.TrimSpace(body)
}

func (m *model) editDiffFile() tea.Cmd {
	file, ok := m.diff.selected()
	if !ok {
		return nil
	}
	target := filepath.Join(m.diffRepo(m.diff.channel).dir, file.Path)
	edit := m.edit
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), gitCallLimit)
		defer cancel()
		if err := edit(ctx, target); err != nil {
			return diffActionMsg{notice: err.Error(), bad: true}
		}
		return nil
	}
}

func (p *diffPanel) selected() (diffFile, bool) {
	if p.sel < 0 || p.sel >= len(p.report.Files) {
		return diffFile{}, false
	}
	return p.report.Files[p.sel], true
}

func (p *diffPanel) clampSel() {
	last := len(p.report.Files) - 1
	if p.sel > last {
		p.sel = last
	}
	if p.sel < 0 {
		p.sel = 0
	}
}

func (p *diffPanel) moveSel(delta int) {
	next := p.sel + delta
	if next < 0 || next >= len(p.report.Files) || next == p.sel {
		return
	}
	p.sel, p.scroll = next, 0
}

func (p *diffPanel) scrollBy(delta int) {
	file, ok := p.selected()
	if !ok {
		return
	}
	p.scroll = clampInt(p.scroll+delta, 0, len(file.Rows)-1)
}

func (p *diffPanel) toggleReviewed() {
	file, ok := p.selected()
	if !ok {
		return
	}
	if p.reviewed[file.Path] {
		delete(p.reviewed, file.Path)
		return
	}
	p.reviewed[file.Path] = true
	p.moveSel(1)
}

func (p *diffPanel) toggleScope() {
	p.sel, p.scroll = 0, 0
	if p.scope == diffScopeTurn {
		p.scope = diffScopeBranch
		return
	}
	p.scope = diffScopeTurn
}

func (p *diffPanel) reviewedCount() int {
	count := 0
	for _, file := range p.report.Files {
		if p.reviewed[file.Path] {
			count++
		}
	}
	return count
}

func clampInt(value, low, high int) int {
	if high < low {
		return low
	}
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func dropLastRune(text string) string {
	_, size := utf8.DecodeLastRuneInString(text)
	return text[:len(text)-size]
}

func typedText(msg tea.KeyMsg) (string, bool) {
	if msg.Type != tea.KeyRunes && msg.Type != tea.KeySpace {
		return "", false
	}
	return string(msg.Runes), len(msg.Runes) > 0
}

func (m *model) diffScreen() string {
	lines := []string{m.railRow()}
	lines = append(lines, m.diffBody(m.width, m.height-2)...)
	lines = append(lines, m.diffKeysLine(m.width))
	return strings.Join(lines, "\n")
}

func (m *model) diffBody(width, height int) []string {
	below := m.diffBelow(width)
	middle := m.diffMiddle(width, height-2-len(below))
	lines := append([]string{m.diffHeaderLine(width), ""}, middle...)
	return fitLines(append(lines, below...), height)
}

func fitLines(lines []string, height int) []string {
	if height < 0 {
		height = 0
	}
	if len(lines) > height {
		return lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return lines
}

func (m *model) diffHeaderLine(width int) string {
	p := &m.diff
	title := "Diff du tour"
	if p.review {
		title = "Revue"
	}
	turn := "tour"
	if !p.startedAt.IsZero() {
		turn = "tour " + p.startedAt.Format("15:04")
	}
	branch := "branche"
	turnLabel, branchLabel := m.st.dim.Render(turn), m.st.dim.Render(branch)
	if p.scope == diffScopeTurn {
		turnLabel = m.st.overlaySel.Render(turn)
	} else {
		branchLabel = m.st.overlaySel.Render(branch)
	}
	left := m.st.strong.Render(" "+title) + "   " + turnLabel + "  " + branchLabel + "  " + m.st.dim.Render("b")
	return truncate(left, width)
}

func (m *model) diffKeysLine(width int) string {
	return m.st.dim.Render(truncate(" ⌃D fermer · ↑↓ fichier · espace relu · i consigne · c commit · p PR · b tour ou branche · ⏎ éditeur", width))
}

func (m *model) diffMiddle(width, height int) []string {
	p := &m.diff
	if height < 1 {
		return nil
	}
	if p.err != nil {
		return fitLines([]string{m.st.red.Render(truncate(" "+p.err.Error(), width))}, height)
	}
	if p.loading && len(p.report.Files) == 0 {
		return fitLines([]string{m.st.dim.Render(" chargement…")}, height)
	}
	if width >= diffWideWidth {
		left := m.diffFileColumn(diffListWidth, height)
		right := m.diffPane(width-diffListWidth-1, height)
		return joinColumns(left, diffListWidth, m.st.dim.Render("│"), right)
	}
	list := m.diffStackedList(width)
	summary := m.diffSummary(width)
	pane := m.diffPane(width, height-len(list)-1)
	return append(append(list, summary), pane...)
}

func (m *model) diffStackedList(width int) []string {
	return m.diffFileRows(width, diffStackedFiles)
}

func (m *model) diffFileColumn(width, height int) []string {
	footer := []string{m.diffSummary(width)}
	if m.diff.scope == diffScopeBranch {
		footer = append(footer, m.st.dim.Render(truncate(fmt.Sprintf(" branche : %d commits de plus sur %s", m.diff.report.Ahead, m.diff.report.Base), width)))
	}
	footer = append(footer, "", m.st.dim.Render(truncate(" espace  marquer relu", width)), m.st.dim.Render(truncate(" i      consigne sur ce fichier", width)),
		m.st.dim.Render(truncate(" c      commit du tour", width)), m.st.dim.Render(truncate(" p      commit puis PR", width)), m.st.dim.Render(truncate(" ⏎      ouvrir dans $EDITOR", width)))
	rows := m.diffFileRows(width, maxInt(1, height-len(footer)))
	return fitLines(append(rows, footer...), height)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (m *model) diffFileRows(width, limit int) []string {
	p := &m.diff
	if len(p.report.Files) == 0 {
		return []string{m.st.dim.Render(truncate(" aucun changement dans cette portée", width))}
	}
	start := 0
	if p.sel >= limit {
		start = p.sel - limit + 1
	}
	end := start + limit
	if end > len(p.report.Files) {
		end = len(p.report.Files)
	}
	rows := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		rows = append(rows, m.diffFileRow(i, width))
	}
	return rows
}

func (m *model) diffFileRow(index, width int) string {
	p := &m.diff
	file := p.report.Files[index]
	selected := index == p.sel
	cursor, mark := " ", m.st.dim.Render(glyphOpenFile)
	if p.reviewed[file.Path] {
		mark = m.st.green.Render(glyphReviewed)
	}
	if selected {
		cursor, mark = glyphCursor, glyphOpenFile
		if p.reviewed[file.Path] {
			mark = glyphReviewed
		}
	}
	left := cursor + " " + mark + " " + string(file.Status) + " " + file.Path
	right := fileStats(file)
	row := truncate(joinRow(width, left, right), width)
	if selected {
		return m.st.overlaySel.Render(padTo(row, width))
	}
	return padTo(row, width)
}

func fileStats(file diffFile) string {
	if file.Binary {
		return "binary"
	}
	return fmt.Sprintf("+%d -%d", file.Added, file.Deleted)
}

func (m *model) diffSummary(width int) string {
	p := &m.diff
	totalAdded, totalDeleted := 0, 0
	for _, file := range p.report.Files {
		totalAdded += file.Added
		totalDeleted += file.Deleted
	}
	text := fmt.Sprintf(" %d fichiers +%d -%d · relus %d/%d", len(p.report.Files), totalAdded, totalDeleted, p.reviewedCount(), len(p.report.Files))
	return m.st.dim.Render(truncate(text, width))
}

func (m *model) diffPane(width, height int) []string {
	p := &m.diff
	if height < 1 {
		return nil
	}
	file, ok := p.selected()
	if !ok {
		return fitLines([]string{m.st.dim.Render(truncate(" rien à afficher", width))}, height)
	}
	twoNums := width >= diffTwoNumsWidth
	header := m.st.text.Render(truncate(" "+file.Path+"  "+fileStats(file), width))
	lines := []string{header}
	switch {
	case file.Binary:
		lines = append(lines, m.st.dim.Render(truncate(" fichier binaire : pas de diff texte", width)))
	case len(file.Rows) == 0:
		lines = append(lines, m.st.dim.Render(truncate(" aucun changement textuel", width)))
	default:
		body := height - 1
		start := clampInt(p.scroll, 0, len(file.Rows)-1)
		end := start + body
		if end > len(file.Rows) {
			end = len(file.Rows)
		}
		for _, row := range file.Rows[start:end] {
			lines = append(lines, m.diffRowLine(row, width, twoNums))
		}
	}
	return fitLines(lines, height)
}

func (m *model) diffRowLine(row diffRow, width int, twoNums bool) string {
	numbers := " "
	if twoNums {
		numbers = padNum(row.Old) + " " + padNum(row.New) + " "
	} else {
		shown := row.New
		if row.Kind == '-' {
			shown = row.Old
		}
		numbers = padNum(shown) + " "
	}
	if row.Kind == '@' {
		return m.st.dim.Render(truncate(strings.Repeat(" ", lipgloss.Width(numbers)+2)+row.Text, width))
	}
	marker := string(row.Kind)
	if row.Kind == ' ' {
		marker = " "
	}
	text := strings.ReplaceAll(row.Text, "\t", "    ")
	line := truncate(numbers+marker+" "+text, width)
	switch row.Kind {
	case '+':
		return m.st.green.Render(line)
	case '-':
		return m.st.red.Render(line)
	}
	return m.st.text.Render(line)
}

func padNum(n int) string {
	if n == 0 {
		return strings.Repeat(" ", diffNumWidth)
	}
	return fmt.Sprintf("%*d", diffNumWidth, n)
}

func joinColumns(left []string, width int, sep string, right []string) []string {
	rows := maxInt(len(left), len(right))
	out := make([]string, rows)
	for i := range out {
		var l, r string
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		out[i] = padTo(truncate(l, width), width) + sep + r
	}
	return out
}

func (m *model) diffBelow(width int) []string {
	p := &m.diff
	var lines []string
	switch p.dlg {
	case dlgCommit:
		lines = m.diffCommitBox(width)
	case dlgNote:
		lines = m.diffNoteBox(width)
	}
	if p.notice != "" {
		style := m.st.green
		if p.noticeBad {
			style = m.st.red
		}
		lines = append(lines, style.Render(truncate(" "+p.notice, width)))
	}
	return lines
}

func (m *model) diffCommitBox(width int) []string {
	p := &m.diff
	files := len(p.report.Files)
	title := m.st.strong.Render("Commit") + m.st.dim.Render(fmt.Sprintf(" · %s · %d fichiers %s", p.report.Branch, files, fileTotals(p.report.Files)))
	content := []string{title, m.st.dim.Render("auteur " + p.report.Author)}
	for i, line := range strings.Split(p.message, "\n") {
		if i == 0 {
			line = m.st.strong.Render(line)
		}
		content = append(content, m.st.text.Render(line))
	}
	if p.editing {
		content = append(content, m.st.dim.Render("ctrl+j nouvelle ligne · échap fin d'édition · ⏎ commit"))
	} else {
		content = append(content, m.st.dim.Render("⏎ commit · p commit puis PR · e modifier · échap annuler"))
	}
	return boxLines(content, width)
}

func fileTotals(files []diffFile) string {
	added, deleted := 0, 0
	for _, file := range files {
		added += file.Added
		deleted += file.Deleted
	}
	return fmt.Sprintf("+%d -%d", added, deleted)
}

func (m *model) diffNoteBox(width int) []string {
	p := &m.diff
	file, _ := p.selected()
	title := m.st.strong.Render(glyphNote+" CONSIGNE") + m.st.dim.Render(" · "+file.Path)
	field := m.st.text.Render(p.field) + m.st.accent.Render(glyphTextCursor)
	if p.field == "" {
		field = m.st.dim.Render("que doit faire l'agent de ce fichier ?") + m.st.accent.Render(glyphTextCursor)
	}
	content := []string{title, m.st.text.Render("› ") + field, m.st.dim.Render("⏎ envoyer à " + p.session + " · échap annuler")}
	return boxLines(content, width)
}

func boxLines(content []string, width int) []string {
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Width(width - 2).Render(strings.Join(content, "\n"))
	return strings.Split(box, "\n")
}
