package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const (
	eventMenu            = "menu"
	eventApproval        = "approval"
	eventApprovalRisk    = "approval_risk"
	approvalTitle        = "! Approbation"
	approvalDefaultWait  = 5 * time.Minute
	approvalQuickWait    = 5 * time.Second
	approvalUrgentWindow = time.Minute
	approvalReasonMax    = 200
	approvalEarlyTTL     = 10 * time.Minute
	approvalMaxWidth     = 240
	approvalTwoRowWidth  = 80
	approvalShortRows    = 24
	approvalNoteColumn   = 34
	approvalPromptTail   = " (answer with"
	approvalAllowPrefix  = "allow "
)

const (
	approvalAllow        = "allow"
	approvalAllowSession = "allow-session"
	approvalAllowAlways  = "allow-always"
	approvalDeny         = "deny"
	riskLowName          = "low"
	riskMediumName       = "medium"
	riskHighName         = "high"
)

const (
	halves            = 2
	approvalFrameCols = 4
	approvalLeadGap   = 2
)

type approvalChoice struct {
	role  string
	key   string
	label string
	note  string
	value string
}

func approvalCatalog() []approvalChoice {
	return []approvalChoice{
		{role: approvalAllow, key: "o", label: "Une fois", note: "seulement cette commande"},
		{role: approvalAllowSession, key: "s", label: "Pour la session", note: "accepté jusqu’à la fin de la session"},
		{role: approvalAllowAlways, key: "t", label: "Toujours", note: "règle ajoutée à l’hôte"},
		{role: approvalDeny, key: "r", label: "Refuser avec une raison", note: "la raison repart à l’agent comme un message"},
	}
}

type approvalPrompt struct {
	id         string
	channel    string
	tool       string
	command    string
	project    string
	worktree   string
	risk       string
	choices    []approvalChoice
	cursor     int
	deadline   time.Time
	reasonOpen bool
	reason     string
}

type approvalDue struct {
	deadline time.Time
	keep     time.Time
}

type approvalRating struct {
	level string
	keep  time.Time
}

type approvalMenuOption struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type approvalMenu struct {
	Prompt  string               `json:"prompt"`
	Risk    string               `json:"risk,omitempty"`
	Options []approvalMenuOption `json:"options"`
}

func (m *model) approvalRiskLine(risk string) string {
	switch risk {
	case riskLowName:
		return m.st.dim.Render("  juge      ") + m.st.green.Render("risque faible")
	case riskMediumName:
		return m.st.dim.Render("  juge      ") + m.st.warn.Render("risque moyen")
	case riskHighName:
		return m.st.dim.Render("  juge      ") + m.st.red.Render("risque élevé")
	}
	return ""
}

func approvalRole(value string) string {
	idx := strings.LastIndex(value, ":")
	if idx < 0 {
		return ""
	}
	role := value[idx+1:]
	for _, entry := range approvalCatalog() {
		if entry.role == role {
			return role
		}
	}
	return ""
}

func splitApprovalSubject(prompt string) (string, string) {
	text, _, _ := strings.Cut(prompt, approvalPromptTail)
	text = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(text), approvalAllowPrefix), "?")
	tool, rest, found := strings.Cut(text, "(")
	if !found {
		return text, ""
	}
	return tool, strings.TrimSuffix(rest, ")")
}

func approvalID(value, role string) string {
	withoutRole := strings.TrimSuffix(value, ":"+role)
	return withoutRole[strings.LastIndex(withoutRole, ":")+1:]
}

func newApprovalPrompt(channel string, menu approvalMenu, session contracts.SessionInfo, now time.Time) (approvalPrompt, bool) {
	prompt := approvalPrompt{
		channel:  channel,
		project:  session.Project,
		worktree: session.Dir,
		deadline: now.Add(approvalDefaultWait),
		risk:     menu.Risk,
	}
	prompt.tool, prompt.command = splitApprovalSubject(menu.Prompt)
	for _, entry := range approvalCatalog() {
		for _, option := range menu.Options {
			if approvalRole(option.Value) != entry.role {
				continue
			}
			entry.value = option.Value
			prompt.id = approvalID(option.Value, entry.role)
			if entry.role == approvalAllowAlways && option.Label != "" && option.Label != approvalAllowAlways {
				entry.note = option.Label
			}
			prompt.choices = append(prompt.choices, entry)
			break
		}
	}
	return prompt, len(prompt.choices) > 0
}

func (ap *approvalPrompt) choice(role string) (approvalChoice, bool) {
	for _, entry := range ap.choices {
		if entry.role == role {
			return entry, true
		}
	}
	return approvalChoice{}, false
}

func (ap *approvalPrompt) moveCursor(delta int) {
	ap.cursor = min(max(ap.cursor+delta, 0), len(ap.choices)-1)
}

func (m *model) sessionInfo(channel string) contracts.SessionInfo {
	if m.tm == nil {
		return contracts.SessionInfo{}
	}
	sessions := m.tm.Sessions()
	for index := range sessions {
		if sessions[index].ChannelID == channel {
			return sessions[index]
		}
	}
	return contracts.SessionInfo{}
}

func (m *model) openApproval(target *tab, event contracts.Event) bool {
	var menu approvalMenu
	if err := json.Unmarshal([]byte(event.Value), &menu); err != nil {
		return false
	}
	prompt, ok := newApprovalPrompt(target.channel, menu, m.sessionInfo(target.channel), time.Now())
	if !ok {
		return false
	}
	target.approval = prompt
	target.pendingApproval = true
	if target.channel != m.active {
		target.unread = true
	}
	m.applyKnownRisk(target)
	m.applyKnownDeadline(target)
	m.applySize()
	return true
}

func sanitizeApprovalReason(reason string) string {
	flat := strings.Join(strings.Fields(reason), " ")
	runes := []rune(flat)
	if len(runes) > approvalReasonMax {
		return string(runes[:approvalReasonMax])
	}
	return flat
}

func (m *model) answerApproval(target *tab, role, reason string) {
	choice, ok := target.approval.choice(role)
	if !ok {
		return
	}
	value := choice.value
	if role == approvalDeny {
		if text := sanitizeApprovalReason(reason); text != "" {
			value += ":" + text
		}
	}
	channel := target.approval.channel
	if channel == "" {
		channel = target.channel
	}
	if m.tm != nil {
		m.tm.Pick(channel, value)
	}
	target.pendingApproval = false
	target.approval = approvalPrompt{}
	m.applySize()
	m.syncViewport()
}

func (m *model) rememberApprovalDeadline(event contracts.Event, now time.Time) {
	due, err := time.Parse(time.RFC3339, event.Text)
	if err != nil || event.Value == "" {
		return
	}
	if m.approvalDues == nil {
		m.approvalDues = make(map[string]approvalDue)
	}
	m.approvalDues[event.Value] = approvalDue{deadline: due, keep: now.Add(approvalEarlyTTL)}
	for _, channel := range m.order {
		if target := m.tabs[channel]; target != nil {
			m.applyKnownDeadline(target)
		}
	}
}

func (m *model) rememberApprovalRisk(event contracts.Event, now time.Time) {
	if event.Value == "" {
		return
	}
	if m.approvalRisks == nil {
		m.approvalRisks = make(map[string]approvalRating)
	}
	m.approvalRisks[event.Value] = approvalRating{level: event.Text, keep: now.Add(approvalEarlyTTL)}
	for _, channel := range m.order {
		if target := m.tabs[channel]; target != nil {
			m.applyKnownRisk(target)
		}
	}
}

func (m *model) applyKnownRisk(target *tab) {
	if !target.pendingApproval || target.approval.id == "" || target.approval.risk != "" {
		return
	}
	known, ok := m.approvalRisks[target.approval.id]
	if !ok {
		return
	}
	target.approval.risk = known.level
	delete(m.approvalRisks, target.approval.id)
}

func (m *model) applyKnownDeadline(target *tab) {
	if !target.pendingApproval || target.approval.id == "" {
		return
	}
	known, ok := m.approvalDues[target.approval.id]
	if !ok {
		return
	}
	target.approval.deadline = known.deadline
	delete(m.approvalDues, target.approval.id)
}

func (m *model) pruneApprovalDeadlines(now time.Time) {
	for key, known := range m.approvalRisks {
		if now.After(known.keep) {
			delete(m.approvalRisks, key)
		}
	}
	for key, known := range m.approvalDues {
		if now.After(known.keep) {
			delete(m.approvalDues, key)
		}
	}
}

func (m *model) expireApprovals(now time.Time) {
	m.pruneApprovalDeadlines(now)
	for _, channel := range m.order {
		target := m.tabs[channel]
		if target == nil || !target.pendingApproval || now.Before(target.approval.deadline) {
			continue
		}
		m.answerApproval(target, approvalDeny, "")
		m.flash = "approbation refusée par défaut : personne n’a répondu à temps"
	}
}

func (m *model) approvalKeysOwned() (*tab, bool) {
	target := m.tabs[m.active]
	if target == nil || !target.pendingApproval {
		return nil, false
	}
	return target, target.approval.reasonOpen || m.input.Value() == ""
}

func (m *model) handleApprovalKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	target, owned := m.approvalKeysOwned()
	if !owned {
		return nil, false
	}
	if target.approval.reasonOpen {
		m.handleApprovalReasonKey(target, msg)
		return nil, true
	}
	switch msg.Type {
	case tea.KeyUp:
		target.approval.moveCursor(-1)
	case tea.KeyDown:
		target.approval.moveCursor(1)
	case tea.KeyEnter:
		m.chooseApproval(target, target.approval.cursor)
	case tea.KeyRunes:
		return m.approvalShortcut(target, string(msg.Runes))
	default:
		return nil, false
	}
	return nil, true
}

func (m *model) approvalShortcut(target *tab, key string) (tea.Cmd, bool) {
	if key == "x" {
		target.approval.deadline = accelerateDeadline(target.approval.deadline, time.Now())
		return nil, true
	}
	for index, entry := range target.approval.choices {
		if entry.key == key {
			m.chooseApproval(target, index)
			return nil, true
		}
	}
	return nil, false
}

func accelerateDeadline(deadline, now time.Time) time.Time {
	if quick := now.Add(approvalQuickWait); deadline.After(quick) {
		return quick
	}
	return deadline
}

func (m *model) chooseApproval(target *tab, index int) {
	choice := target.approval.choices[index]
	if choice.role == approvalDeny {
		target.approval.reasonOpen = true
		return
	}
	m.answerApproval(target, choice.role, "")
}

func (m *model) handleApprovalReasonKey(target *tab, msg tea.KeyMsg) {
	prompt := &target.approval
	switch msg.Type {
	case tea.KeyEsc:
		prompt.reasonOpen = false
	case tea.KeyEnter:
		m.answerApproval(target, approvalDeny, prompt.reason)
	case tea.KeyBackspace:
		if runes := []rune(prompt.reason); len(runes) > 0 {
			prompt.reason = string(runes[:len(runes)-1])
		}
	case tea.KeySpace:
		prompt.reason += " "
	case tea.KeyRunes:
		prompt.reason += string(msg.Runes)
	}
}

func approvalCountdown(deadline, now time.Time) (string, time.Duration) {
	remaining := max(deadline.Sub(now), 0)
	seconds := int((remaining + time.Second - 1) / time.Second)
	return fmt.Sprintf("refus dans %d:%02d", seconds/secondsPerMinute, seconds%secondsPerMinute), remaining
}

func middleTruncate(text string, width int, ellipsis string) string {
	if lipgloss.Width(text) <= width {
		return text
	}
	runes := []rune(text)
	for keep := width - 1; keep >= 0; keep-- {
		head := (keep + 1) / halves
		candidate := string(runes[:head]) + ellipsis + string(runes[len(runes)-(keep-head):])
		if lipgloss.Width(candidate) <= width {
			return candidate
		}
	}
	return ""
}

func (m *model) approvalLines(now time.Time) []string {
	target := m.tabs[m.active]
	if target == nil || !target.pendingApproval {
		return nil
	}
	width := min(m.innerWidth(), approvalMaxWidth)
	if width < approvalTwoRowWidth-approvalNoteColumn {
		return nil
	}
	box := m.st.boxes(m.caps.Boxes)
	inner := width - approvalFrameCols
	prompt := target.approval
	lines := []string{m.approvalTop(prompt, width, box, now)}
	row := func(content string) { lines = append(lines, approvalRow(box, content, inner)) }
	row(m.approvalCommand(prompt, inner))
	if prompt.project != "" {
		row(m.st.dim.Render("  projet    ") + prompt.project)
	}
	if prompt.worktree != "" {
		row(m.st.dim.Render("  worktree  ") + prompt.worktree)
	}
	if line := m.approvalRiskLine(prompt.risk); line != "" {
		row(line)
	}
	if !m.approvalShort() {
		row("")
	}
	for index, entry := range prompt.choices {
		for _, line := range m.approvalChoiceLines(entry, index == prompt.cursor, width >= approvalTwoRowWidth) {
			row(line)
		}
	}
	if prompt.reasonOpen {
		if !m.approvalShort() {
			row("")
		}
		row(m.approvalReasonLine(prompt))
	}
	lines = append(lines, m.approvalBottom(prompt, width, box))
	return lines
}

func approvalRow(box boxSet, content string, inner int) string {
	return box.v + " " + padTo(truncate(content, inner), inner) + " " + box.v
}

func (m *model) approvalTop(prompt approvalPrompt, width int, box boxSet, now time.Time) string {
	countdown, remaining := approvalCountdown(prompt.deadline, now)
	urgency := m.st.green
	if remaining < approvalUrgentWindow {
		urgency = m.st.red
	}
	title := m.st.green.Bold(true).Render(approvalTitle)
	if width >= approvalTwoRowWidth {
		title += m.st.dim.Render(" · demandée par l’hôte")
	}
	head := box.tl + box.h + " " + title + " "
	tail := " " + urgency.Render(countdown) + " " + box.h + box.tr
	return m.approvalBorder(head, tail, box.h, width)
}

func (m *model) approvalBottom(prompt approvalPrompt, width int, box boxSet) string {
	keys := make([]string, 0, len(prompt.choices))
	for _, entry := range prompt.choices {
		keys = append(keys, entry.key)
	}
	footer := "↑↓ choisir · " + m.st.glyphs.enter + " valider · " + strings.Join(keys, " ") + " · x accélérer le délai"
	head := box.bl + box.h + " " + m.st.dim.Render(footer) + " "
	tail := box.br
	return m.approvalBorder(head, tail, box.h, width)
}

func (m *model) approvalBorder(head, tail, fill string, width int) string {
	gap := width - lipgloss.Width(head) - lipgloss.Width(tail)
	if gap < 0 {
		return truncate(head+tail, width)
	}
	return head + m.st.dim.Render(strings.Repeat(fill, gap)) + tail
}

func (m *model) approvalCommand(prompt approvalPrompt, inner int) string {
	lead := m.st.accent.Render(m.st.glyphs.familyGlyph(familyRun)) + " " + m.st.text.Bold(true).Render(prompt.tool)
	if prompt.command == "" {
		return lead
	}
	room := inner - lipgloss.Width(lead) - approvalLeadGap
	return lead + "  " + m.st.text.Render(middleTruncate(prompt.command, room, m.st.glyphs.ellipsis))
}

func (m *model) approvalChoiceLines(entry approvalChoice, selected, wide bool) []string {
	mark := "  "
	labelStyle := m.st.text
	if selected {
		mark = m.st.accent.Render(m.st.glyphs.cursor) + " "
		labelStyle = m.st.accent.Bold(true)
	}
	head := mark + m.st.green.Render(entry.key) + "  " + labelStyle.Render(entry.label)
	if wide {
		return []string{padTo(head, approvalNoteColumn) + m.st.dim.Render(entry.note)}
	}
	if m.approvalShort() {
		return []string{head + "  " + m.st.dim.Render(entry.note)}
	}
	return []string{head, "     " + m.st.dim.Render(entry.note)}
}

func (m *model) approvalShort() bool {
	return m.height < approvalShortRows
}

func (m *model) approvalReasonLine(prompt approvalPrompt) string {
	line := m.st.dim.Render("  raison "+m.st.glyphs.reasonArrow+" ") + m.st.text.Render(prompt.reason) + m.st.accent.Render(m.st.glyphs.caret)
	if prompt.reason == "" {
		line += m.st.dim.Render("raison envoyée à l’agent")
	}
	return line + m.st.dim.Render("   "+m.st.glyphs.enter+" envoyer · échap revenir")
}
