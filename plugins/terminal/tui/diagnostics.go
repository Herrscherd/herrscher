package tui

import (
	"strings"
)

type diagRow struct {
	name    string
	value   string
	feature string
}

func diagRows(trans translator, caps Capabilities, paging string) []diagRow {
	graphics := diagRow{name: trans.t("tui.diag.graphics", nil), value: caps.Graphics.String()}
	switch caps.Graphics {
	case GraphicsKitty:
		graphics.feature = trans.t("tui.diag.graphics_kitty", nil)
	case GraphicsSixel:
		graphics.feature = trans.t("tui.diag.graphics_sixel", nil)
	case GraphicsNone:
		graphics.feature = trans.t("tui.diag.graphics_none", nil)
	}

	links := diagRow{name: trans.t("tui.diag.links", nil), value: yesNo(trans, caps.Hyperlinks), feature: trans.t("tui.diag.links_on", nil)}
	if !caps.Hyperlinks {
		links.feature = trans.t("tui.diag.links_off", nil)
	}

	mouse := diagRow{name: trans.t("tui.diag.mouse", nil), value: yesNo(trans, caps.Mouse), feature: trans.t("tui.diag.mouse_on", nil)}
	if !caps.Mouse {
		mouse.feature = trans.t("tui.diag.mouse_off", map[string]any{"Paging": paging})
	}

	colour := diagRow{name: trans.t("tui.diag.colour", nil), value: caps.Colour.String(), feature: trans.t("tui.diag.colour_true", nil)}
	if caps.Colour != ColourTrue {
		colour.feature = trans.t("tui.diag.colour_reduced", nil)
	}

	return []diagRow{graphics, links, mouse, colour}
}

func yesNo(trans translator, b bool) string {
	if b {
		return trans.t("tui.common.yes", nil)
	}
	return trans.t("tui.common.no", nil)
}

const diagValueWidth = 12

func diagnosticsView(trans translator, caps Capabilities, paging string, styles uiStyles) string {
	var builder strings.Builder
	builder.WriteString(styles.dim.Render(trans.t("tui.diag.terminal", nil)) + "  " + styles.text.Render(caps.Terminal))
	for _, row := range diagRows(trans, caps, paging) {
		pad := strings.Repeat(" ", max(1, diagValueWidth-len(row.value)))
		builder.WriteString("\n" + styles.dim.Render("  "+row.name+strings.Repeat(" ", max(1, factWidth-len(row.name)))) +
			styles.text.Render(row.value) + pad + styles.dim.Render(row.feature))
	}
	builder.WriteString("\n" + styles.dim.Render("  "+trans.t("tui.diag.note_one", nil)))
	builder.WriteString("\n" + styles.dim.Render("  "+trans.t("tui.diag.note_two", nil)))
	return builder.String()
}

func (m *model) openDiagnostics() { m.diagOpen = true }

func (m *model) diagView() string {
	if !m.diagOpen {
		return ""
	}
	return m.st.dim.Render(joinNonEmpty(m.tr.t("tui.diag.title", nil), m.labelledHint(actDiagnosticsClose, m.tr.t("tui.common.close", nil), ctxDiagnostics))) + "\n" + diagnosticsView(m.tr, m.caps, joinWith("/", m.hintIn(actScrollPageUp, chatKeyStack()...), m.hintIn(actScrollPageDown, chatKeyStack()...)), m.st) +
		"\n" + m.remoteImageRow()
}

func (m *model) remoteImageRow() string {
	value, feature := m.tr.t("tui.diag.remote_off", nil), m.tr.t("tui.diag.remote_off_feature", nil)
	if m.imageFetcher != nil && len(m.imageHosts) > 0 {
		value = m.tr.t("tui.diag.remote_on", nil)
		feature = m.tr.t("tui.diag.remote_on_feature", map[string]any{"Hosts": strings.Join(m.imageHosts, ", ")})
	}
	name := m.tr.t("tui.diag.remote_images", nil)
	return m.st.dim.Render("  "+name+strings.Repeat(" ", max(1, factWidth-len(name)))) +
		m.st.text.Render(value) + strings.Repeat(" ", max(1, diagValueWidth-len(value))) +
		m.st.dim.Render(feature)
}
