package tui

import (
	"strings"
)

const maxComposerLines = 8

func (m *model) composerHeight() int {
	if m.composerRows >= 1 {
		return m.composerRows
	}
	return 1
}

func (m *model) resizeComposer() {
	height := displayRows(m.input.Value(), m.input.Width())
	if height < 1 {
		height = 1
	}
	if height > maxComposerLines {
		height = maxComposerLines
	}
	height = m.fitComposerRows(height)
	if height != m.composerRows {
		m.composerRows = height
		m.applySize()
		m.syncViewport()
	}
}

func (m *model) fitComposerRows(want int) int {
	if m.height <= 0 {
		return want
	}
	saved := m.composerRows
	defer func() { m.composerRows = saved }()
	for rows := want; rows > 1; rows-- {
		m.composerRows = rows
		if m.chromeHeight() <= m.height-minTranscriptRows {
			return rows
		}
	}
	return 1
}

func (m *model) chromeHeight() int {
	return m.buildFrame().height()
}

func (m *model) innerWidth() int {
	if m.width < 1 {
		return 1
	}
	return m.width
}

func (m *model) applySize() {
	if !m.ready {
		return
	}
	m.vp.Width = m.transcriptWidth()
	m.vp.Height = max(0, m.height-m.chromeHeight())
}

func (m *model) frameTop() int {
	top := 0
	for _, line := range m.buildFrame().above {
		top += strings.Count(line, "\n") + 1
	}
	return top
}
