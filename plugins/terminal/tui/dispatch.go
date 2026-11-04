package tui

import tea "github.com/charmbracelet/bubbletea"

type keyRun func(*model, tea.KeyMsg) tea.Cmd

type chatRun func(*model, tea.KeyMsg) (tea.Cmd, bool)

type flagRun func(*model, tea.KeyMsg) bool

type navRun func(*model, func(int)) bool

type approvalRun func(*model, *tab)

type modelRun func(*model) tea.Cmd

type dispatchTables struct {
	approvalReason  map[keyAction]approvalRun
	approval        map[keyAction]approvalRun
	mention         map[keyAction]flagRun
	palette         map[keyAction]chatRun
	resume          map[keyAction]keyRun
	skills          map[keyAction]keyRun
	diag            map[keyAction]keyRun
	search          map[keyAction]keyRun
	switchOverlay   map[keyAction]keyRun
	pendingClose    map[keyAction]keyRun
	themeLook       map[keyAction]modelRun
	themeScreen     map[keyAction]keyRun
	statuslineField map[keyAction]keyRun
	statusline      map[keyAction]keyRun
	rewindSure      map[keyAction]keyRun
	rewindOptions   map[keyAction]keyRun
	rewindList      map[keyAction]keyRun
	queue           map[keyAction]flagRun
	plugins         map[keyAction]keyRun
	panes           map[keyAction]chatRun
	board           map[keyAction]keyRun
	plan            map[keyAction]keyRun
	context         map[keyAction]keyRun
	nav             map[keyAction]navRun
	diffMessage     map[keyAction]keyRun
	diffDialog      map[keyAction]keyRun
	diffNote        map[keyAction]keyRun
	diffPanel       map[keyAction]keyRun
	chat            map[keyAction]chatRun
	histSearch      map[keyAction]keyRun
}

func newDispatchTables() *dispatchTables {
	return &dispatchTables{
		approvalReason:  approvalReasonRuns(),
		approval:        approvalRuns(),
		mention:         mentionRuns(),
		palette:         paletteRuns(),
		resume:          resumeRuns(),
		skills:          skillsRuns(),
		diag:            diagRuns(),
		search:          searchRuns(),
		switchOverlay:   switchOverlayRuns(),
		pendingClose:    pendingCloseRuns(),
		themeLook:       themeLookRuns(),
		themeScreen:     themeScreenRuns(),
		statuslineField: statuslineFieldRuns(),
		statusline:      statuslineRuns(),
		rewindSure:      rewindSureRuns(),
		rewindOptions:   rewindOptionsRuns(),
		rewindList:      rewindListRuns(),
		queue:           queueRuns(),
		plugins:         pluginsRuns(),
		panes:           panesRuns(),
		board:           boardRuns(),
		plan:            planRuns(),
		context:         contextRuns(),
		nav:             navRuns(),
		diffMessage:     diffMessageRuns(),
		diffDialog:      diffDialogRuns(),
		diffNote:        diffNoteRuns(),
		diffPanel:       diffPanelRuns(),
		chat:            chatRuns(),
		histSearch:      histSearchRuns(),
	}
}

func (m *model) tables() *dispatchTables {
	if m.handlers == nil {
		m.handlers = newDispatchTables()
	}
	return m.handlers
}

func stepKey(step func(*model)) keyRun {
	return func(m *model, _ tea.KeyMsg) tea.Cmd {
		step(m)
		return nil
	}
}

func cmdKey(run func(*model) tea.Cmd) keyRun {
	return func(m *model, _ tea.KeyMsg) tea.Cmd {
		return run(m)
	}
}

func stepChat(step func(*model)) chatRun {
	return func(m *model, _ tea.KeyMsg) (tea.Cmd, bool) {
		step(m)
		return nil, true
	}
}

func chatCmd(run func(*model) tea.Cmd) chatRun {
	return func(m *model, _ tea.KeyMsg) (tea.Cmd, bool) {
		return run(m), true
	}
}

func stepFlag(step func(*model)) flagRun {
	return func(m *model, _ tea.KeyMsg) bool {
		step(m)
		return true
	}
}

func dispatchTable[Entry any](fill func(set func(keyAction, Entry))) map[keyAction]Entry {
	table := make(map[keyAction]Entry)
	fill(func(action keyAction, run Entry) { table[action] = run })
	return table
}
