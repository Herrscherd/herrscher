package tui

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	keyBackspace = "backspace"
	keyEnter     = "enter"
	keyEsc       = "esc"
	keyPgDown    = "pgdown"
	keyPgUp      = "pgup"
	keySpace     = " "
)

const (
	diffWideWidth      = 100
	diffListWidth      = 46
	diffNumWidth       = 4
	diffTwoNumsWidth   = 70
	diffStackedFiles   = 6
	diffPageRows       = 10
	diffHunkIndent     = 2
	boxBorderWidth     = 2
	diffPanelTabSpaces = "    "
)

type diffDialog int

const (
	dialogNone diffDialog = iota
	dialogNote
	dialogCommit
)

type diffPanel struct {
	open      bool
	review    bool
	channel   string
	session   string
	scope     diffScope
	startedAt time.Time
	gen       int
	loading   bool
	report    diffReport
	err       error
	sel       int
	scroll    int
	reviewed  map[string]bool
	dlg       diffDialog
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
	err     error
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
		for index := range sessions {
			if sessions[index].Name == name && sessions[index].Dir != "" {
				return sessions[index].Dir
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
		tree, err := repo.snapshot(ctx)
		return turnStartMsg{channel: channel, tree: tree, err: err}
	}
}

func (m *model) openDiff(review bool) tea.Cmd {
	chat := m.tabs[m.active]
	if chat == nil {
		m.flash = "no session open"
		return nil
	}
	panel := &m.diff
	*panel = diffPanel{
		open:      true,
		review:    review,
		channel:   m.active,
		session:   m.sessionName(m.active),
		scope:     diffScopeTurn,
		startedAt: chat.startedAt,
		gen:       panel.gen,
		reviewed:  make(map[string]bool),
	}
	return m.loadDiffCmd()
}

func (m *model) loadDiffCmd() tea.Cmd {
	panel := &m.diff
	panel.gen++
	panel.loading = true
	gen, scope, repo := panel.gen, panel.scope, m.diffRepo(panel.channel)
	turnStart := ""
	if chat := m.tabs[panel.channel]; chat != nil {
		turnStart = chat.turnStart
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), diffLoadLimit)
		defer cancel()
		report, err := repo.loadDiff(ctx, scope, turnStart)
		return diffLoadedMsg{gen: gen, report: report, err: err}
	}
}

func (m *model) handleDiffMsg(msg tea.Msg) (tea.Cmd, bool) {
	panel := &m.diff
	switch msg := msg.(type) {
	case diffLoadedMsg:
		if msg.gen == panel.gen && panel.open {
			panel.loading = false
			panel.err = msg.err
			panel.report = msg.report
			panel.clampSel()
		}
		return nil, true
	case diffActionMsg:
		panel.busy = false
		panel.notice, panel.noticeBad = msg.notice, msg.bad
		if panel.open {
			return m.loadDiffCmd(), true
		}
		return nil, true
	case turnStartMsg:
		m.releaseQueuedTurns(msg)
		return nil, true
	}
	return nil, false
}

func (m *model) releaseQueuedTurns(msg turnStartMsg) {
	chat := m.tabs[msg.channel]
	if chat == nil {
		return
	}
	if msg.err != nil {
		m.flash = "instantané du tour impossible : " + msg.err.Error()
	}
	chat.turnStart = msg.tree
	for _, turn := range chat.queued {
		m.tm.Submit(msg.channel, chat.withShellContext(turn.text), turn.atts)
	}
	chat.queued, chat.snapshotting = nil, false
}

func (m *model) handleDiffKey(msg tea.KeyMsg) tea.Cmd {
	switch m.diff.dlg {
	case dialogNote:
		return m.handleDiffNoteKey(msg)
	case dialogCommit:
		return m.handleDiffCommitKey(msg)
	case dialogNone:
	}
	return m.handleDiffPanelKey(msg)
}

func (m *model) handleDiffPanelKey(msg tea.KeyMsg) tea.Cmd {
	chosen := m.stackAction(msg, ctxDiffPanel)
	if run, ok := m.tables().diffPanel[chosen]; ok {
		return run(m, msg)
	}
	return nil
}

func diffPanelRuns() (map[keyAction]keyRun, error) {
	return dispatchOf(
		bind[keyRun](actDiffDismiss, stepKey(func(app *model) { app.diff.open = false })),
		bind[keyRun](actInterrupt, cmdKey(func(app *model) tea.Cmd {
			cmd, _ := app.interruptOrQuit()
			return cmd
		})),
		bind[keyRun](actDiffPrevFile, stepKey(func(app *model) { app.diff.moveSel(-1) })),
		bind[keyRun](actDiffNextFile, stepKey(func(app *model) { app.diff.moveSel(1) })),
		bind[keyRun](actScrollPageUp, stepKey(func(app *model) { app.diff.scrollBy(-diffPageRows) })),
		bind[keyRun](actScrollPageDown, stepKey(func(app *model) { app.diff.scrollBy(diffPageRows) })),
		bind[keyRun](actDiffReviewed, stepKey(func(app *model) { app.diff.toggleReviewed() })),
		bind[keyRun](actDiffOpenFile, cmdKey((*model).editDiffFile)),
		bind[keyRun](actDiffNote, stepKey(openDiffNote)),
		bind[keyRun](actDiffCommit, stepKey(openDiffCommit)),
		bind[keyRun](actDiffPR, cmdKey(draftAndCommitPR)),
		bind[keyRun](actCycleDiffBase, cmdKey(toggleDiffBase)),
	)
}

func openDiffNote(app *model) {
	if _, ok := app.diff.selected(); ok {
		app.diff.dlg, app.diff.field = dialogNote, ""
	}
}

func openDiffCommit(m *model) {
	m.diff.dlg, m.diff.message, m.diff.editing = dialogCommit, draftCommitMessage(m.diff.report.Files), false
}

func draftAndCommitPR(app *model) tea.Cmd {
	app.diff.message = draftCommitMessage(app.diff.report.Files)
	return app.commitCmd(true)
}

func toggleDiffBase(app *model) tea.Cmd {
	app.diff.toggleScope()
	return app.loadDiffCmd()
}

func (m *model) handleDiffNoteKey(msg tea.KeyMsg) tea.Cmd {
	chosen := m.stackAction(msg, ctxDiffNote)
	if run, ok := m.tables().diffNote[chosen]; ok {
		return run(m, msg)
	}
	m.diff.field = editDraft(m.diff.field, msg)
	return nil
}

func diffNoteRuns() (map[keyAction]keyRun, error) {
	return dispatchOf(
		bind[keyRun](actInterrupt, cmdKey((*model).overlayCtrlC)),
		bind[keyRun](actDiffDismiss, stepKey(func(app *model) { app.diff.dlg = dialogNone })),
		bind[keyRun](actDiffSendNote, cmdKey((*model).sendDiffNote)),
	)
}

func (m *model) handleDiffCommitKey(msg tea.KeyMsg) tea.Cmd {
	if m.diff.editing {
		return m.handleDiffMessageEdit(msg)
	}
	chosen := m.stackAction(msg, ctxDiffDialog)
	if run, ok := m.tables().diffDialog[chosen]; ok {
		return run(m, msg)
	}
	return nil
}

func diffDialogRuns() (map[keyAction]keyRun, error) {
	return dispatchOf(
		bind[keyRun](actInterrupt, cmdKey((*model).overlayCtrlC)),
		bind[keyRun](actDiffDismiss, stepKey(func(app *model) { app.diff.dlg = dialogNone })),
		bind[keyRun](actDiffCommit, cmdKey(func(app *model) tea.Cmd { return app.commitCmd(false) })),
		bind[keyRun](actDiffPR, cmdKey(func(app *model) tea.Cmd { return app.commitCmd(true) })),
		bind[keyRun](actDiffEdit, stepKey(func(app *model) { app.diff.editing = true })),
	)
}

func (m *model) handleDiffMessageEdit(msg tea.KeyMsg) tea.Cmd {
	chosen := m.stackAction(msg, ctxDiffMessage)
	if run, ok := m.tables().diffMessage[chosen]; ok {
		return run(m, msg)
	}
	m.diff.message = editDraft(m.diff.message, msg)
	return nil
}

func diffMessageRuns() (map[keyAction]keyRun, error) {
	return dispatchOf(
		bind[keyRun](actInterrupt, cmdKey((*model).overlayCtrlC)),
		bind[keyRun](actDiffStopEdit, stepKey(func(app *model) { app.diff.editing = false })),
		bind[keyRun](actDiffCommit, cmdKey(func(app *model) tea.Cmd { return app.commitCmd(false) })),
		bind[keyRun](actNewline, stepKey(func(app *model) { app.diff.message += "\n" })),
	)
}

func (m *model) sendDiffNote() tea.Cmd {
	panel := &m.diff
	file, ok := panel.selected()
	text := strings.TrimSpace(panel.field)
	if !ok || text == "" {
		return nil
	}
	panel.dlg, panel.field = dialogNone, ""
	panel.notice, panel.noticeBad = "consigne envoyée sur "+file.Path, false
	return m.submitTo(panel.channel, file.Path+": "+text, nil)
}

func editDraft(text string, msg tea.KeyMsg) string {
	if msg.Type == tea.KeyBackspace {
		return dropLastRune(text)
	}
	if typed, ok := typedText(msg); ok {
		return text + typed
	}
	return text
}

func (m *model) commitCmd(withPR bool) tea.Cmd {
	panel := &m.diff
	message := strings.TrimSpace(panel.message)
	if panel.busy {
		return nil
	}
	if message == "" {
		panel.dlg = dialogNone
		panel.notice, panel.noticeBad = "rien à commiter dans cette portée", true
		return nil
	}
	panel.busy, panel.dlg, panel.editing = true, dialogNone, false
	subject, body := splitMessage(message)
	repo, base := m.diffRepo(panel.channel), panel.report.Base
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

func (panel *diffPanel) selected() (diffFile, bool) {
	if panel.sel < 0 || panel.sel >= len(panel.report.Files) {
		return diffFile{}, false
	}
	return panel.report.Files[panel.sel], true
}

func (panel *diffPanel) clampSel() {
	panel.sel = clampIndex(panel.sel, len(panel.report.Files)-1)
}

func (panel *diffPanel) moveSel(delta int) {
	next := panel.sel + delta
	if next < 0 || next >= len(panel.report.Files) || next == panel.sel {
		return
	}
	panel.sel, panel.scroll = next, 0
}

func (panel *diffPanel) scrollBy(delta int) {
	file, ok := panel.selected()
	if !ok {
		return
	}
	panel.scroll = clampIndex(panel.scroll+delta, len(file.Rows)-1)
}

func (panel *diffPanel) toggleReviewed() {
	file, ok := panel.selected()
	if !ok {
		return
	}
	if panel.reviewed[file.Path] {
		delete(panel.reviewed, file.Path)
		return
	}
	panel.reviewed[file.Path] = true
	panel.moveSel(1)
}

func (panel *diffPanel) toggleScope() {
	panel.sel, panel.scroll = 0, 0
	if panel.scope == diffScopeTurn {
		panel.scope = diffScopeBranch
		return
	}
	panel.scope = diffScopeTurn
}

func (panel *diffPanel) reviewedCount() int {
	count := 0
	for _, file := range panel.report.Files {
		if panel.reviewed[file.Path] {
			count++
		}
	}
	return count
}

func clampIndex(value, last int) int {
	return min(max(value, 0), max(last, 0))
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
	top := m.railLines()
	bottom := append([]string{m.st.rule(m.st.dim, m.width), m.diffKeysLine(m.width)}, m.statusZone()...)
	body := m.diffBody(m.width, m.height-len(top)-len(bottom))
	return strings.Join(slices.Concat(top, body, bottom), "\n")
}

func (m *model) diffBody(width, height int) []string {
	below := m.diffBelow(width)
	middle := m.diffMiddle(width, height-2-len(below))
	lines := append([]string{m.diffHeaderLine(width), ""}, middle...)
	return fitLines(append(lines, below...), height)
}

func fitLines(lines []string, height int) []string {
	height = max(height, 0)
	if len(lines) > height {
		return lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return lines
}

func (m *model) diffHeaderLine(width int) string {
	panel := &m.diff
	title := "Diff du tour"
	if panel.review {
		title = "Revue"
	}
	turn := "tour"
	if !panel.startedAt.IsZero() {
		turn = "tour " + panel.startedAt.Format("15:04")
	}
	branch := "branche"
	turnLabel, branchLabel := m.st.dim.Render(turn), m.st.dim.Render(branch)
	if panel.scope == diffScopeTurn {
		turnLabel = m.st.overlaySel.Render(turn)
	} else {
		branchLabel = m.st.overlaySel.Render(branch)
	}
	left := m.st.strong.Render(" "+title) + "   " + turnLabel + "  " + branchLabel + "  " + m.st.dim.Render("b")
	return truncate(left, width)
}

const diffKeysContext = "comme /review, en panneau "

func (m *model) diffKeysLine(width int) string {
	hints := m.diffFooterHints()
	full := " " + strings.Join(hints, " · ")
	if lipgloss.Width(full)+1+lipgloss.Width(diffKeysContext) <= width {
		return m.st.dim.Render(joinRow(width, full, diffKeysContext))
	}
	return m.st.dim.Render(foldKeyHints(hints, width))
}

func (m *model) diffFooterHints() []string {
	return []string{
		m.labelledHint(actDiffDismiss, "fermer le panneau", ctxDiffPanel),
		m.pairHint(actDiffPrevFile, actDiffNextFile, "fichier", ctxDiffPanel),
		m.labelledHint(actDiffReviewed, "relu", ctxDiffPanel),
		m.labelledHint(actDiffNote, "consigne", ctxDiffPanel),
		m.labelledHint(actDiffCommit, "commit", ctxDiffPanel),
		m.labelledHint(actDiffPR, "PR", ctxDiffPanel),
		m.labelledHint(actCycleDiffBase, "tour ou branche", ctxDiffPanel),
	}
}

func foldKeyHints(hints []string, width int) string {
	shown := ""
	for count := 1; count <= len(hints); count++ {
		candidate := " " + strings.Join(hints[:count], " · ")
		if lipgloss.Width(candidate) > width {
			break
		}
		shown = candidate
	}
	if shown == "" {
		return ansi.Truncate(" "+hints[0], width, "…")
	}
	return shown
}

func (m *model) diffMiddle(width, height int) []string {
	panel := &m.diff
	if height < 1 {
		return nil
	}
	if panel.err != nil {
		return fitLines([]string{m.st.red.Render(truncate(" "+panel.err.Error(), width))}, height)
	}
	if panel.loading && len(panel.report.Files) == 0 {
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
	panel := &m.diff
	footer := []string{m.diffSummary(width)}
	if panel.scope == diffScopeBranch {
		footer = append(footer, m.st.dim.Render(truncate(fmt.Sprintf("   branche : %d commits de plus sur %s", panel.report.Ahead, panel.report.Base), width)))
	}
	footer = append(footer, "",
		m.diffAction(actDiffReviewed, "marquer relu", width),
		m.diffAction(actDiffNote, "consigne sur ce fichier", width),
		m.diffAction(actDiffCommit, "commit du tour", width),
		m.diffAction(actDiffPR, "commit puis PR", width),
		m.diffAction(actDiffOpenFile, "ouvrir dans $EDITOR", width))
	rows := m.diffFileRows(width, max(1, height-len(footer)))
	return fitLines(append(rows, footer...), height)
}

func (m *model) diffAction(action keyAction, label string, width int) string {
	return m.st.dim.Render(truncate(fmt.Sprintf("   %-7s%s", m.hintIn(action, ctxDiffPanel), label), width))
}

func (m *model) diffFileRows(width, limit int) []string {
	panel := &m.diff
	if len(panel.report.Files) == 0 {
		return []string{m.st.dim.Render(truncate(" aucun changement dans cette portée", width))}
	}
	start := 0
	if panel.sel >= limit {
		start = panel.sel - limit + 1
	}
	end := min(start+limit, len(panel.report.Files))
	rows := make([]string, 0, end-start)
	for index := start; index < end; index++ {
		rows = append(rows, m.diffFileRow(index, width))
	}
	return rows
}

func (m *model) diffFileRow(index, width int) string {
	panel := &m.diff
	file := panel.report.Files[index]
	selected := index == panel.sel
	reviewed := panel.reviewed[file.Path]
	cursor, mark := " ", m.st.dim.Render(m.st.glyphs.open)
	if reviewed {
		mark = m.st.green.Render(m.st.glyphs.success)
	}
	if selected {
		cursor, mark = m.st.glyphs.cursor, m.st.glyphs.open
		if reviewed {
			mark = m.st.glyphs.success
		}
	}
	prefix := " " + cursor + " " + mark + " "
	stats := m.fileStats(file, selected)
	pathWidth := width - lipgloss.Width(prefix) - lipgloss.Width(stats) - 1
	left := prefix + truncateLeft(panel.displayPath(file.Path), pathWidth)
	gap := m.paint(lipgloss.NewStyle(), strings.Repeat(" ", max(width-lipgloss.Width(left)-lipgloss.Width(stats), 1)), selected)
	return truncate(m.paint(lipgloss.NewStyle(), left, selected)+gap+stats, width)
}

func (m *model) fileStats(file diffFile, selected bool) string {
	if file.Binary {
		return m.paint(m.st.dim, "binary", selected)
	}
	return m.signedStats(file.Added, file.Deleted, selected)
}

func (m *model) signedStats(added, deleted int, selected bool) string {
	plus := m.paint(m.st.green, fmt.Sprintf("+%d", added), selected)
	if deleted == 0 {
		return plus
	}
	minus := m.paint(m.st.red, m.st.glyphs.minus+strconv.Itoa(deleted), selected)
	return plus + m.paint(lipgloss.NewStyle(), " ", selected) + minus
}

func (m *model) paint(style lipgloss.Style, text string, selected bool) string {
	if selected {
		style = style.Inherit(m.st.overlaySel)
	}
	return style.Render(text)
}

func (panel *diffPanel) displayPath(filePath string) string {
	return strings.TrimPrefix(filePath, commonDirPrefix(panel.report.Files))
}

func commonDirPrefix(files []diffFile) string {
	if len(files) == 0 {
		return ""
	}
	dir := path.Dir(files[0].Path)
	for _, file := range files[1:] {
		for dir != "." && !strings.HasPrefix(file.Path, dir+"/") {
			dir = path.Dir(dir)
		}
	}
	if dir == "." {
		return ""
	}
	return dir + "/"
}

func (m *model) diffSummary(width int) string {
	panel := &m.diff
	text := m.st.dim.Render(fmt.Sprintf("   %d fichiers ", len(panel.report.Files))) + m.totalStats(panel.report.Files) +
		m.st.dim.Render(fmt.Sprintf(" · relus %d/%d", panel.reviewedCount(), len(panel.report.Files)))
	return truncate(text, width)
}

func (m *model) totalStats(files []diffFile) string {
	added, deleted := 0, 0
	for _, file := range files {
		added += file.Added
		deleted += file.Deleted
	}
	return m.signedStats(added, deleted, false)
}

func (m *model) diffPane(width, height int) []string {
	panel := &m.diff
	if height < 1 {
		return nil
	}
	file, ok := panel.selected()
	if !ok {
		return fitLines([]string{m.st.dim.Render(truncate(" rien à afficher", width))}, height)
	}
	twoNums := width >= diffTwoNumsWidth
	lines := []string{m.diffPaneHeader(file, width)}
	switch {
	case file.Binary:
		lines = append(lines, m.st.dim.Render(truncate(" fichier binaire : pas de diff texte", width)))
	case len(file.Rows) == 0:
		lines = append(lines, m.st.dim.Render(truncate(" aucun changement textuel", width)))
	default:
		start := clampIndex(panel.scroll, len(file.Rows)-1)
		end := min(start+height-1, len(file.Rows))
		for _, row := range file.Rows[start:end] {
			lines = append(lines, m.diffRowLine(row, width, twoNums))
		}
	}
	return fitLines(lines, height)
}

func (m *model) diffPaneHeader(file diffFile, width int) string {
	right := m.fileStats(file, false) + m.st.dim.Render("   ancien · nouveau ")
	tag := ""
	if file.Status == changeAdded {
		tag = "  nouveau"
	}
	pathWidth := width - lipgloss.Width(right) - lipgloss.Width(tag) - 1
	left := m.st.text.Render(truncateLeft(" "+file.Path, pathWidth)) + m.st.violet.Render(tag)
	return truncate(joinRow(width, left, right), width)
}

func (m *model) diffRowLine(row gitRow, width int, twoNums bool) string {
	numbers := rowNumbers(row, twoNums)
	if row.Kind == rowHunk {
		return m.st.dim.Render(truncate(strings.Repeat(" ", lipgloss.Width(numbers)+diffHunkIndent)+row.Text, width))
	}
	line := truncate(numbers+row.Kind.String()+" "+strings.ReplaceAll(row.Text, "\t", diffPanelTabSpaces), width)
	switch row.Kind {
	case rowAdded:
		return m.st.green.Render(line)
	case rowRemoved:
		return m.st.red.Render(line)
	case rowContext, rowHunk:
	}
	return m.st.text.Render(line)
}

func rowNumbers(row gitRow, twoNums bool) string {
	if twoNums {
		return padNum(row.Old) + " " + padNum(row.New) + " "
	}
	shown := row.New
	if row.Kind == rowRemoved {
		shown = row.Old
	}
	return padNum(shown) + " "
}

func padNum(number int) string {
	if number == 0 {
		return strings.Repeat(" ", diffNumWidth)
	}
	return fmt.Sprintf("%*d", diffNumWidth, number)
}

func joinColumns(left []string, width int, sep string, right []string) []string {
	rows := max(len(left), len(right))
	out := make([]string, rows)
	for index := range out {
		var leftCell, rightCell string
		if index < len(left) {
			leftCell = left[index]
		}
		if index < len(right) {
			rightCell = right[index]
		}
		out[index] = padTo(truncate(leftCell, width), width) + sep + rightCell
	}
	return out
}

func (m *model) diffBelow(width int) []string {
	panel := &m.diff
	var lines []string
	switch panel.dlg {
	case dialogCommit:
		lines = m.diffCommitBox(width)
	case dialogNote:
		lines = m.diffNoteBox(width)
	case dialogNone:
	}
	if panel.notice != "" {
		style := m.st.green
		if panel.noticeBad {
			style = m.st.red
		}
		lines = append(lines, style.Render(truncate(" "+panel.notice, width)))
	}
	return lines
}

func (m *model) diffCommitBox(width int) []string {
	panel := &m.diff
	files := len(panel.report.Files)
	title := m.st.strong.Render("Commit") + m.st.dim.Render(fmt.Sprintf(" · %s · %d fichiers ", panel.report.Branch, files)) + m.totalStats(panel.report.Files)
	content := []string{title, m.st.dim.Render("auteur " + panel.report.Author)}
	for index, line := range strings.Split(panel.message, "\n") {
		if index == 0 {
			line = m.st.strong.Render(line)
		}
		content = append(content, m.st.text.Render(line))
	}
	if panel.editing {
		content = append(content, m.st.dim.Render(joinNonEmpty(m.labelledHint(actNewline, "nouvelle ligne", ctxDiffMessage), m.labelledHint(actDiffStopEdit, "fin d’édition", ctxDiffMessage), m.labelledHint(actDiffCommit, "commit", ctxDiffMessage))))
	} else {
		content = append(content, m.st.dim.Render(joinNonEmpty(m.labelledHint(actDiffCommit, "commit", ctxDiffDialog), m.labelledHint(actDiffPR, "commit puis PR", ctxDiffDialog), m.labelledHint(actDiffEdit, "modifier", ctxDiffDialog), m.labelledHint(actDiffDismiss, "annuler", ctxDiffDialog))))
	}
	return boxLines(content, width)
}

func (m *model) diffNoteBox(width int) []string {
	panel := &m.diff
	file, _ := panel.selected()
	title := m.st.strong.Render(m.st.glyphs.note+" CONSIGNE") + m.st.dim.Render(" · "+panel.displayPath(file.Path))
	field := m.st.text.Render(panel.field) + m.st.accent.Render(m.st.glyphs.caret)
	if panel.field == "" {
		field = m.st.dim.Render("que doit faire l’agent de ce fichier ?") + m.st.accent.Render(m.st.glyphs.caret)
	}
	content := []string{title, m.st.text.Render("› ") + field, m.st.dim.Render(joinNonEmpty(m.labelledHint(actDiffSendNote, "envoyer à "+panel.session, ctxDiffNote), m.labelledHint(actDiffDismiss, "annuler", ctxDiffNote)))}
	return boxLines(content, width)
}

func boxLines(content []string, width int) []string {
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Width(width - boxBorderWidth).Render(strings.Join(content, "\n"))
	return strings.Split(box, "\n")
}
