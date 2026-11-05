package tui

import (
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type keyRun func(*model, tea.KeyMsg) tea.Cmd

type chatRun func(*model, tea.KeyMsg) (tea.Cmd, bool)

type flagRun func(*model, tea.KeyMsg) bool

type navRun func(*model, func(int)) bool

type approvalRun func(*model, *tab) bool

type reasonRun func(*model, *tab)

type modelRun func(*model) tea.Cmd

type dispatchTables struct {
	approvalReason  map[keyAction]reasonRun
	approval        map[keyAction]approvalRun
	toast           map[keyAction]keyRun
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

func newDispatchTables() (*dispatchTables, error) {
	tables := &dispatchTables{}
	var errs []error
	var err error
	tables.approvalReason, err = approvalReasonRuns()
	errs = append(errs, err)
	tables.approval, err = approvalRuns()
	errs = append(errs, err)
	tables.mention, err = mentionRuns()
	errs = append(errs, err)
	tables.palette, err = paletteRuns()
	errs = append(errs, err)
	tables.resume, err = resumeRuns()
	errs = append(errs, err)
	tables.skills, err = skillsRuns()
	errs = append(errs, err)
	tables.diag, err = diagRuns()
	errs = append(errs, err)
	tables.search, err = searchRuns()
	errs = append(errs, err)
	tables.switchOverlay, err = switchOverlayRuns()
	errs = append(errs, err)
	tables.pendingClose, err = pendingCloseRuns()
	errs = append(errs, err)
	tables.themeLook, err = themeLookRuns()
	errs = append(errs, err)
	tables.themeScreen, err = themeScreenRuns()
	errs = append(errs, err)
	tables.statuslineField, err = statuslineFieldRuns()
	errs = append(errs, err)
	tables.statusline, err = statuslineRuns()
	errs = append(errs, err)
	tables.rewindSure, err = rewindSureRuns()
	errs = append(errs, err)
	tables.rewindOptions, err = rewindOptionsRuns()
	errs = append(errs, err)
	tables.rewindList, err = rewindListRuns()
	errs = append(errs, err)
	tables.queue, err = queueRuns()
	errs = append(errs, err)
	tables.plugins, err = pluginsRuns()
	errs = append(errs, err)
	tables.panes, err = panesRuns()
	errs = append(errs, err)
	tables.board, err = boardRuns()
	errs = append(errs, err)
	tables.plan, err = planRuns()
	errs = append(errs, err)
	tables.context, err = contextRuns()
	errs = append(errs, err)
	tables.nav, err = navRuns()
	errs = append(errs, err)
	tables.diffMessage, err = diffMessageRuns()
	errs = append(errs, err)
	tables.diffDialog, err = diffDialogRuns()
	errs = append(errs, err)
	tables.diffNote, err = diffNoteRuns()
	errs = append(errs, err)
	tables.diffPanel, err = diffPanelRuns()
	errs = append(errs, err)
	tables.chat, err = chatRuns()
	errs = append(errs, err)
	tables.histSearch, err = histSearchRuns()
	errs = append(errs, err)
	tables.toast, err = toastRuns()
	errs = append(errs, err)
	return tables, errors.Join(errs...)
}

var errDuplicateBinding = errors.New("action liée deux fois dans une même table")

type keyBinding[Run any] struct {
	action keyAction
	run    Run
}

func bind[Run any](action keyAction, run Run) keyBinding[Run] {
	return keyBinding[Run]{action: action, run: run}
}

func dispatchOf[Run any](bindings ...keyBinding[Run]) (map[keyAction]Run, error) {
	table := make(map[keyAction]Run, len(bindings))
	var duplicates []string
	for _, binding := range bindings {
		if _, taken := table[binding.action]; taken {
			duplicates = append(duplicates, string(binding.action))
			continue
		}
		table[binding.action] = binding.run
	}
	if len(duplicates) > 0 {
		return table, fmt.Errorf("%w : %s", errDuplicateBinding, strings.Join(duplicates, ", "))
	}
	return table, nil
}

func (m *model) tables() *dispatchTables {
	if m.handlers == nil {
		m.installTables()
	}
	return m.handlers
}

func (m *model) installTables() {
	tables, err := m.buildTables()
	m.handlers = tables
	if err != nil {
		m.keyProblems = append(m.keyProblems, m.tr.t("tui.keys.inconsistent", map[string]any{tplArgReason: err.Error()}))
	}
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
