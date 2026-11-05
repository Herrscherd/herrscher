package tui

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const (
	escDoubleWindow    = 700 * time.Millisecond
	rewindMinSplit     = 80
	rewindListGap      = 3
	rewindOptionGap    = 2
	rewindFileIndent   = "      "
	rewindFooterIndent = 2
	noConversation     = "none"
	argTo              = "--to"
	argWhat            = "--what"
)

const rewindUnsupportedNote = "conversation non reprise : le backend de cette session ne garde pas de position par message"

type rewindStep int

const (
	rewindStepList rewindStep = iota + 1
	rewindStepOptions
	rewindStepSure
)

type rewindOption int

const (
	rewindOptionBoth rewindOption = iota + 1
	rewindOptionConversation
	rewindOptionCode
	rewindOptionFork
	rewindOptionCancel
)

type rewindOptionSpec struct {
	label             string
	detail            string
	needsConversation bool
}

func rewindOptionOrder() []rewindOption {
	return []rewindOption{rewindOptionBoth, rewindOptionConversation, rewindOptionCode, rewindOptionFork, rewindOptionCancel}
}

func (option rewindOption) spec() rewindOptionSpec {
	switch option {
	case rewindOptionBoth:
		return rewindOptionSpec{label: "Restaurer le code et la conversation", detail: "le code et la conversation reviennent à ce message", needsConversation: true}
	case rewindOptionConversation:
		return rewindOptionSpec{label: "Restaurer la conversation seulement", detail: "la conversation reprend à ce message", needsConversation: true}
	case rewindOptionCode:
		return rewindOptionSpec{label: "Restaurer le code seulement", detail: "la conversation reste"}
	case rewindOptionFork:
		return rewindOptionSpec{label: "Forker à partir d’ici", detail: "/fork : nouvelle session, l’originale intacte"}
	case rewindOptionCancel:
		return rewindOptionSpec{label: "Annuler"}
	}
	return rewindOptionSpec{}
}

type rewindWhat int

const (
	rewindWhatCode rewindWhat = iota + 1
	rewindWhatBoth
	rewindWhatConversation
)

func (what rewindWhat) String() string {
	switch what {
	case rewindWhatCode:
		return "code"
	case rewindWhatBoth:
		return "both"
	case rewindWhatConversation:
		return "conversation"
	}
	return ""
}

func (what rewindWhat) doneLabel() string {
	switch what {
	case rewindWhatCode:
		return "code restauré"
	case rewindWhatBoth:
		return "code et conversation restaurés"
	case rewindWhatConversation:
		return "conversation reprise"
	}
	return ""
}

type rewindFile struct {
	Path    string `json:"path"`
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
}

type rewindRow struct {
	Index        int          `json:"index"`
	At           string       `json:"at"`
	Turn         string       `json:"turn"`
	Text         string       `json:"text"`
	Conversation string       `json:"conversation"`
	Files        []rewindFile `json:"files"`
}

func (row rewindRow) conversationRestorable() bool {
	return row.Conversation != "" && row.Conversation != noConversation
}

type rewindDoneMsg struct {
	session string
	text    string
	what    rewindWhat
	index   int
	err     error
}

type forkDoneMsg struct {
	name string
	err  error
}

func (m *model) escapeIdle() {
	if m.input.Value() != "" {
		m.escAt = time.Time{}
		return
	}
	now := time.Now()
	if !m.escAt.IsZero() && now.Sub(m.escAt) < escDoubleWindow {
		m.escAt = time.Time{}
		m.openRewind()
		return
	}
	m.escAt = now
}

func (m *model) openRewind() {
	name := m.sessionName(m.active)
	if name == "" {
		m.flash = "retour : aucune session ouverte"
		return
	}
	out, err := m.tm.Dispatch([]string{sessionVerb, "checkpoints", argName, name, "--json"})
	if err != nil {
		m.flash = "retour : " + err.Error()
		return
	}
	var rows []rewindRow
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		m.flash = "retour : réponse illisible"
		return
	}
	if len(rows) == 0 {
		m.flash = "retour : aucun checkpoint pour " + name
		return
	}
	m.rwRows = rows
	m.rwIdx = len(rows) - 1
	m.rwOpt = rewindOptionBoth
	m.rwStep = rewindStepList
	m.rwOpen = true
	m.relayout()
}

func (m *model) closeRewind() {
	m.rwOpen = false
	m.relayout()
}

func (m *model) rewindSelected() (rewindRow, bool) {
	if m.rwIdx < 0 || m.rwIdx >= len(m.rwRows) {
		return rewindRow{}, false
	}
	return m.rwRows[m.rwIdx], true
}

func (m *model) handleRewindKey(msg tea.KeyMsg) tea.Cmd {
	switch m.rwStep {
	case rewindStepList:
		return m.handleRewindList(msg)
	case rewindStepOptions:
		return m.handleRewindOptions(msg)
	case rewindStepSure:
		return m.handleRewindSure(msg)
	}
	return nil
}

func (m *model) handleRewindList(msg tea.KeyMsg) tea.Cmd {
	action := m.stackAction(msg, ctxMessageSelector)
	if m.overlayNav(action, m.moveRewind) {
		return nil
	}
	if run, ok := m.tables().rewindList[action]; ok {
		return run(m, msg)
	}
	return nil
}

func rewindListRuns() (map[keyAction]keyRun, error) {
	return dispatchOf(
		bind[keyRun](actSelectAccept, stepKey(func(app *model) {
			app.rwStep = rewindStepOptions
			app.rwOpt = rewindOptionBoth
		})),
		bind[keyRun](actRewindFork, cmdKey(func(app *model) tea.Cmd {
			app.rwOpt = rewindOptionFork
			return app.chooseRewindOption()
		})),
		bind[keyRun](actRewindUndo, cmdKey((*model).undoRewind)),
		bind[keyRun](actSelectCancel, stepKey((*model).closeRewind)),
		bind[keyRun](actInterrupt, cmdKey((*model).overlayCtrlC)),
	)
}

func (m *model) handleRewindOptions(msg tea.KeyMsg) tea.Cmd {
	action := m.stackAction(msg, ctxMessageSelector)
	if m.overlayNav(action, m.moveRewindOpt) {
		return nil
	}
	if run, ok := m.tables().rewindOptions[action]; ok {
		return run(m, msg)
	}
	return nil
}

func rewindOptionsRuns() (map[keyAction]keyRun, error) {
	return dispatchOf(
		bind[keyRun](actSelectAccept, cmdKey((*model).chooseRewindOption)),
		bind[keyRun](actSelectCancel, stepKey(func(app *model) { app.rwStep = rewindStepList })),
		bind[keyRun](actInterrupt, cmdKey((*model).overlayCtrlC)),
	)
}

func (m *model) handleRewindSure(msg tea.KeyMsg) tea.Cmd {
	chosen := m.stackAction(msg, ctxMessageSelector)
	if run, ok := m.tables().rewindSure[chosen]; ok {
		return run(m, msg)
	}
	return nil
}

func rewindSureRuns() (map[keyAction]keyRun, error) {
	return dispatchOf(
		bind[keyRun](actConfirmYes, cmdKey((*model).restoreRewind)),
		bind[keyRun](actConfirmNo, stepKey(func(app *model) { app.rwStep = rewindStepOptions })),
		bind[keyRun](actSelectCancel, stepKey(func(app *model) { app.rwStep = rewindStepOptions })),
		bind[keyRun](actInterrupt, cmdKey((*model).overlayCtrlC)),
	)
}

func (m *model) moveRewind(delta int) {
	m.rwIdx = clampIndex(m.rwIdx+delta, len(m.rwRows)-1)
}

func (m *model) moveRewindOpt(delta int) {
	order := rewindOptionOrder()
	m.rwOpt = order[clampIndex(slices.Index(order, m.rwOpt)+delta, len(order)-1)]
}

func (m *model) chooseRewindOption() tea.Cmd {
	row, ok := m.rewindSelected()
	switch m.rwOpt {
	case rewindOptionCancel:
		m.closeRewind()
	case rewindOptionFork:
		return m.forkFromRewind()
	case rewindOptionCode:
		m.rwWhat = rewindWhatCode
		m.rwStep = rewindStepSure
	case rewindOptionBoth, rewindOptionConversation:
		if !ok || !row.conversationRestorable() {
			m.flash = rewindUnsupportedNote
			return nil
		}
		m.rwWhat = rewindWhatBoth
		if m.rwOpt == rewindOptionConversation {
			m.rwWhat = rewindWhatConversation
		}
		m.rwStep = rewindStepSure
	}
	return nil
}

func (m *model) rewindName() string { return m.sessionName(m.active) }

func (m *model) restoreRewind() tea.Cmd {
	row, ok := m.rewindSelected()
	name := m.rewindName()
	if !ok || name == "" {
		return nil
	}
	backend, index, text, what := m.tm, row.Index, row.Text, m.rwWhat
	m.closeRewind()
	return func() tea.Msg {
		_, err := backend.Dispatch([]string{sessionVerb, "rewind", argName, name, argTo, strconv.Itoa(index), argWhat, what.String()})
		return rewindDoneMsg{session: name, text: text, what: what, index: index, err: err}
	}
}

func (m *model) undoRewind() tea.Cmd {
	name := m.rewindName()
	if name == "" {
		return nil
	}
	backend, origin := m.tm, m.active
	m.closeRewind()
	return func() tea.Msg {
		out, err := backend.Dispatch([]string{sessionVerb, "undo", argName, name})
		return dispatchResultMsg{origin: origin, out: out, err: err}
	}
}

func (m *model) forkFromRewind() tea.Cmd {
	row, ok := m.rewindSelected()
	name := m.rewindName()
	if !ok || name == "" {
		return nil
	}
	backend := m.tm
	index := row.Index
	m.closeRewind()
	return func() tea.Msg {
		out, err := backend.Dispatch([]string{sessionVerb, "fork", argName, name, argTo, strconv.Itoa(index), "--json"})
		var forked struct {
			Name string `json:"name"`
		}
		if err == nil {
			err = json.Unmarshal([]byte(out), &forked)
		}
		return forkDoneMsg{name: forked.Name, err: err}
	}
}

func (m *model) handleRewindDone(msg rewindDoneMsg) {
	if msg.err != nil {
		m.flash = "retour refusé : " + msg.err.Error()
		return
	}
	if m.input.Value() == "" {
		m.input.SetValue(msg.text)
	}
	m.flash = fmt.Sprintf("revenu au message %d · %s", msg.index, msg.what.doneLabel())
}

func (m *model) handleForkDone(msg forkDoneMsg) {
	if msg.err != nil {
		m.flash = "fork refusé : " + msg.err.Error()
		return
	}
	channel := channelOfSession(m.tm.Sessions(), msg.name)
	if channel == "" {
		m.flash = "fork créé : " + msg.name
		return
	}
	m.ensureTab(channel)
	m.active = channel
	m.syncViewport()
	m.flash = "fork ouvert : " + msg.name
}

func channelOfSession(sessions []contracts.SessionInfo, name string) string {
	for index := range sessions {
		if sessions[index].Name == name && sessions[index].ChannelID != "" {
			return sessions[index].ChannelID
		}
	}
	return ""
}

func (m *model) rewindView() string {
	width := m.overlayWidth()
	lines := append([]string{m.rewindHeader(width)}, m.rewindBody(width)...)
	return strings.Join(lines, "\n")
}

func (m *model) rewindBody(width int) []string {
	switch m.rwStep {
	case rewindStepList:
		return m.rewindListLines(width)
	case rewindStepOptions:
		return m.rewindOptionLines(width)
	case rewindStepSure:
		return m.rewindSureLines(width)
	}
	return nil
}

func (m *model) rewindHeader(width int) string {
	title := m.st.accent.Render("Revenir en arrière") + m.st.dim.Render(" · "+m.doubleEscGlyphs())
	return cutText("  "+title, width)
}

func (m *model) rewindListLines(width int) []string {
	lines := []string{cutText("  "+m.st.dim.Render("un checkpoint avant chaque message"), width)}
	start, end := paletteWindow(len(m.rwRows), m.rwIdx, m.overlayRows())
	for index := start; index < end; index++ {
		row := m.rwRows[index]
		selected := index == m.rwIdx
		head := "  "
		if selected {
			head = m.st.glyphs.prompt + " "
		}
		summary := rewindSummary(row.Files)
		line := cutText("  "+head+row.At+"  "+row.Text, width)
		if width >= rewindMinSplit {
			line = rightAligned(line, summary, width, rewindListGap)
		}
		lines = append(lines, m.rewindRow(line, selected, width))
		if width < rewindMinSplit && summary != "" {
			lines = append(lines, cutText(rewindFileIndent+m.st.dim.Render(summary), width))
		}
	}
	footer := fitSegments([]string{m.pairHint(actSelectPrevious, actSelectNext, "choisir", ctxMessageSelector), m.labelledHint(actSelectAccept, "revenir à ce message", ctxMessageSelector), m.labelledHint(actRewindFork, "forker d’ici", ctxMessageSelector), m.labelledHint(actRewindUndo, "annuler le retour", ctxMessageSelector), m.labelledHint(actSelectCancel, "fermer", ctxMessageSelector)}, width-rewindFooterIndent)
	return append(lines, "  "+m.st.dim.Render(footer))
}

func (m *model) rewindOptionLines(width int) []string {
	row, ok := m.rewindSelected()
	if !ok {
		return nil
	}
	lines := []string{cutText("  "+m.st.text.Render("Message de "+row.At+" : « "+row.Text+" »"), width)}
	if files := rewindFileNames(row.Files); files != "" {
		lines = append(lines, cutText("  "+m.st.dim.Render("fichiers touchés depuis : ")+m.st.green.Render(files), width))
	}
	for index, option := range rewindOptionOrder() {
		spec := option.spec()
		selected := option == m.rwOpt
		head := "  "
		if selected {
			head = m.st.glyphs.prompt + " "
		}
		label := fmt.Sprintf("%d. %s", index+1, spec.label)
		detail := spec.detail
		if spec.needsConversation && !row.conversationRestorable() {
			detail = rewindUnsupportedNote
		}
		line := "  " + head + label
		if detail != "" {
			line = rightAligned(cutText(line, width), m.st.dim.Render(detail), width, rewindOptionGap)
		}
		lines = append(lines, m.rewindRow(cutText(line, width), selected, width))
	}
	footer := fitSegments([]string{m.pairHint(actSelectPrevious, actSelectNext, "choisir", ctxMessageSelector), m.labelledHint(actSelectAccept, "valider", ctxMessageSelector), m.labelledHint(actSelectCancel, "retour", ctxMessageSelector)}, width-rewindFooterIndent)
	return append(lines, "  "+m.st.dim.Render(footer))
}

func rightAligned(left, right string, width, gap int) string {
	room := width - lipgloss.Width(right) - gap
	return padTo(cutText(left, room), room) + "  " + right
}

func (m *model) rewindSureLines(width int) []string {
	row, ok := m.rewindSelected()
	if !ok {
		return nil
	}
	question := rewindQuestion(m.rwWhat, row.At)
	return []string{
		cutText("  "+m.st.warn.Render(question), width),
		cutText("  "+m.st.dim.Render("y confirmer · n annuler"), width),
	}
}

func rewindQuestion(what rewindWhat, when string) string {
	switch what {
	case rewindWhatBoth:
		return fmt.Sprintf("Restaurer le code et la conversation au message de %s ? Les fichiers reviennent à cet état et la conversation reprend à ce message.", when)
	case rewindWhatConversation:
		return fmt.Sprintf("Restaurer la conversation au message de %s ? Les fichiers ne bougent pas.", when)
	case rewindWhatCode:
		return fmt.Sprintf("Restaurer le code au message de %s ? Les fichiers du worktree reviennent à cet état.", when)
	}
	return ""
}

func (m *model) rewindRow(line string, selected bool, width int) string {
	if selected {
		return m.st.overlaySel.Render(padTo(line, width))
	}
	return m.st.dim.Render(padTo(line, width))
}

func rewindSummary(files []rewindFile) string {
	if len(files) == 0 {
		return "aucun fichier"
	}
	added, removed := 0, 0
	for _, file := range files {
		added += file.Added
		removed += file.Removed
	}
	noun := "fichiers"
	if len(files) == 1 {
		noun = "fichier"
	}
	count := fmt.Sprintf("%d %s", len(files), noun)
	if lines := changedLines(added, removed); lines != "" {
		return lines + " · " + count
	}
	return count
}

func changedLines(added, removed int) string {
	var parts []string
	if added > 0 {
		parts = append(parts, fmt.Sprintf("+%d", added))
	}
	if removed > 0 {
		parts = append(parts, fmt.Sprintf("−%d", removed))
	}
	return strings.Join(parts, " ")
}

func rewindFileNames(files []rewindFile) string {
	names := make([]string, 0, len(files))
	for _, file := range files {
		names = append(names, file.Path)
	}
	return strings.Join(names, ", ")
}

func (m *model) doubleEscGlyphs() string {
	glyph := m.hintIn(actCancel, chatKeyStack()...)
	return joinWith(" ", glyph, glyph)
}
