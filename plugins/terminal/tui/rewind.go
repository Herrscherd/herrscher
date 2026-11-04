package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	escDoubleWindow       = 700 * time.Millisecond
	rewindStepList        = 0
	rewindStepOpts        = 1
	rewindStepSure        = 2
	rewindOptBoth         = 0
	rewindOptConversation = 1
	rewindOptCode         = 2
	rewindOptFork         = 3
	rewindOptCancel       = 4
	rewindMinSplit        = 80
)

var rewindOptions = []struct {
	label, detail     string
	needsConversation bool
}{
	{label: "Restaurer le code et la conversation", detail: "le code et la conversation reviennent à ce message", needsConversation: true},
	{label: "Restaurer la conversation seulement", detail: "la conversation reprend à ce message", needsConversation: true},
	{label: "Restaurer le code seulement", detail: "la conversation reste"},
	{label: "Forker à partir d’ici", detail: "/fork : nouvelle session, l’originale intacte"},
	{label: "Annuler"},
}

const rewindUnsupportedNote = "conversation non reprise : le backend de cette session ne garde pas de position par message"

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
	return row.Conversation != "" && row.Conversation != "none"
}

type rewindDoneMsg struct {
	session string
	text    string
	what    string
	index   int
	err     error
}

type forkDoneMsg struct {
	name string
	err  error
}

func (m *model) escapeIdle() tea.Cmd {
	if m.input.Value() != "" {
		m.escAt = time.Time{}
		return nil
	}
	now := time.Now()
	if !m.escAt.IsZero() && now.Sub(m.escAt) < escDoubleWindow {
		m.escAt = time.Time{}
		m.openRewind()
		return nil
	}
	m.escAt = now
	return nil
}

func (m *model) openRewind() {
	name := m.sessionName(m.active)
	if name == "" {
		m.flash = "retour : aucune session ouverte"
		return
	}
	out, err := m.tm.Dispatch([]string{"session", "checkpoints", "--name", name, "--json"})
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
	m.rwOpt = 0
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
	case rewindStepOpts:
		return m.handleRewindOptions(msg)
	default:
		return m.handleRewindSure(msg)
	}
}

func (m *model) handleRewindList(msg tea.KeyMsg) tea.Cmd {
	if m.overlayNav(msg, m.moveRewind) {
		return nil
	}
	switch msg.String() {
	case "enter":
		m.rwStep = rewindStepOpts
		m.rwOpt = 0
	case "f":
		m.rwOpt = rewindOptFork
		return m.chooseRewindOption()
	case "u":
		return m.undoRewind()
	case "esc":
		m.closeRewind()
	case "ctrl+c":
		return m.overlayCtrlC()
	}
	return nil
}

func (m *model) handleRewindOptions(msg tea.KeyMsg) tea.Cmd {
	if m.overlayNav(msg, m.moveRewindOpt) {
		return nil
	}
	switch msg.Type {
	case tea.KeyEnter:
		return m.chooseRewindOption()
	case tea.KeyEsc:
		m.rwStep = rewindStepList
	case tea.KeyCtrlC:
		return m.overlayCtrlC()
	}
	return nil
}

func (m *model) handleRewindSure(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "y", "Y":
		return m.restoreRewind()
	case "n", "N", "esc":
		m.rwStep = rewindStepOpts
	case "ctrl+c":
		return m.overlayCtrlC()
	}
	return nil
}

func (m *model) moveRewind(delta int) {
	m.rwIdx = clampIndex(m.rwIdx+delta, len(m.rwRows))
}

func (m *model) moveRewindOpt(delta int) {
	m.rwOpt = clampIndex(m.rwOpt+delta, len(rewindOptions))
}

func clampIndex(index, count int) int {
	if index >= count {
		return count - 1
	}
	if index < 0 {
		return 0
	}
	return index
}

func (m *model) chooseRewindOption() tea.Cmd {
	row, ok := m.rewindSelected()
	switch m.rwOpt {
	case rewindOptCancel:
		m.closeRewind()
	case rewindOptFork:
		return m.forkFromRewind()
	case rewindOptCode:
		m.rwWhat = "code"
		m.rwStep = rewindStepSure
	case rewindOptBoth, rewindOptConversation:
		if !ok || !row.conversationRestorable() {
			m.flash = rewindUnsupportedNote
			return nil
		}
		m.rwWhat = "both"
		if m.rwOpt == rewindOptConversation {
			m.rwWhat = "conversation"
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
		_, err := backend.Dispatch([]string{"session", "rewind", "--name", name, "--to", fmt.Sprint(index), "--what", what})
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
		out, err := backend.Dispatch([]string{"session", "undo", "--name", name})
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
		out, err := backend.Dispatch([]string{"session", "fork", "--name", name, "--to", fmt.Sprint(index), "--json"})
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
	m.flash = fmt.Sprintf("revenu au message %d · %s", msg.index, rewindDoneLabel[msg.what])
}

func (m *model) handleForkDone(msg forkDoneMsg) {
	if msg.err != nil {
		m.flash = "fork refusé : " + msg.err.Error()
		return
	}
	for _, session := range m.tm.Sessions() {
		if session.Name != msg.name || session.ChannelID == "" {
			continue
		}
		m.ensureTab(session.ChannelID)
		m.active = session.ChannelID
		m.syncViewport()
		m.flash = "fork ouvert : " + msg.name
		return
	}
	m.flash = "fork créé : " + msg.name
}

func (m *model) rewindView() string {
	width := m.overlayWidth()
	lines := []string{m.rewindHeader(width)}
	switch m.rwStep {
	case rewindStepList:
		lines = append(lines, m.rewindListLines(width)...)
	case rewindStepOpts:
		lines = append(lines, m.rewindOptionLines(width)...)
	default:
		lines = append(lines, m.rewindSureLines(width)...)
	}
	return strings.Join(lines, "\n")
}

func (m *model) rewindHeader(width int) string {
	title := m.st.accent.Render("Revenir en arrière") + m.st.dim.Render(" · échap échap")
	return truncate("  "+title, width)
}

func (m *model) rewindListLines(width int) []string {
	lines := []string{truncate("  "+m.st.dim.Render("un checkpoint avant chaque message"), width)}
	for index, row := range m.rwRows {
		selected := index == m.rwIdx
		head := "  "
		if selected {
			head = glyphCursor + " "
		}
		summary := rewindSummary(row.Files)
		line := head + row.At + "  " + row.Text
		if width >= rewindMinSplit {
			line = padTo(truncate("  "+line, width-lipgloss.Width(summary)-3), width-lipgloss.Width(summary)-3) + "  " + summary
		} else {
			line = truncate("  "+line, width)
		}
		lines = append(lines, m.rewindRow(line, selected, width))
		if width < rewindMinSplit && summary != "" {
			lines = append(lines, truncate("      "+m.st.dim.Render(summary), width))
		}
	}
	footer := "↑↓ choisir · ⏎ revenir à ce message · f forker d’ici · u annuler le retour · échap fermer"
	return append(lines, truncate("  "+m.st.dim.Render(footer), width))
}

var rewindDoneLabel = map[string]string{
	"code":         "code restauré",
	"both":         "code et conversation restaurés",
	"conversation": "conversation reprise",
}

func (m *model) rewindOptionLines(width int) []string {
	row, ok := m.rewindSelected()
	if !ok {
		return nil
	}
	lines := []string{truncate("  "+m.st.text.Render("Message de "+row.At+" : « "+row.Text+" »"), width)}
	if files := rewindFileNames(row.Files); files != "" {
		lines = append(lines, truncate("  "+m.st.dim.Render("fichiers touchés depuis : ")+m.st.green.Render(files), width))
	}
	for index, option := range rewindOptions {
		selected := index == m.rwOpt
		head := "  "
		if selected {
			head = glyphCursor + " "
		}
		label := fmt.Sprintf("%d. %s", index+1, option.label)
		detail := option.detail
		if option.needsConversation && !row.conversationRestorable() {
			detail = rewindUnsupportedNote
		}
		line := "  " + head + label
		if detail != "" {
			line = padTo(truncate(line, width-lipgloss.Width(detail)-2), width-lipgloss.Width(detail)-2) + "  " + m.st.dim.Render(detail)
		}
		lines = append(lines, m.rewindRow(truncate(line, width), selected, width))
	}
	return append(lines, truncate("  "+m.st.dim.Render("↑↓ choisir · ⏎ valider · échap retour"), width))
}

func (m *model) rewindSureLines(width int) []string {
	row, ok := m.rewindSelected()
	if !ok {
		return nil
	}
	question := rewindQuestion(m.rwWhat, row.At)
	return []string{
		truncate("  "+m.st.warn.Render(question), width),
		truncate("  "+m.st.dim.Render("y confirmer · n annuler"), width),
	}
}

func rewindQuestion(what, at string) string {
	switch what {
	case "both":
		return fmt.Sprintf("Restaurer le code et la conversation au message de %s ? Les fichiers reviennent à cet état et la conversation reprend à ce message.", at)
	case "conversation":
		return fmt.Sprintf("Restaurer la conversation au message de %s ? Les fichiers ne bougent pas.", at)
	default:
		return fmt.Sprintf("Restaurer le code au message de %s ? Les fichiers du worktree reviennent à cet état.", at)
	}
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
	return fmt.Sprintf("+%d −%d · %d %s", added, removed, len(files), noun)
}

func rewindFileNames(files []rewindFile) string {
	names := make([]string, 0, len(files))
	for _, file := range files {
		names = append(names, file.Path)
	}
	return strings.Join(names, ", ")
}
