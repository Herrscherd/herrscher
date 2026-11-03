package tui

import (
	"strings"
)

type diagRow struct {
	name    string
	value   string
	feature string
}

func diagRows(caps Capabilities) []diagRow {
	graphics := diagRow{name: "graphismes", value: caps.Graphics.String()}
	switch caps.Graphics {
	case GraphicsKitty:
		graphics.feature = "images en ligne, pleine résolution"
	case GraphicsSixel:
		graphics.feature = "images en ligne, résolution sixel"
	case GraphicsNone:
		graphics.feature = "images en ligne en demi-blocs unicode"
	}

	links := diagRow{name: "hyperliens", value: yesNo(caps.Hyperlinks), feature: "les liens s'ouvrent depuis le terminal (OSC 8)"}
	if !caps.Hyperlinks {
		links.feature = "les liens sont du texte stylé, ouverts à la touche"
	}

	mouse := diagRow{name: "souris", value: yesNo(caps.Mouse), feature: "molette et clic pour choisir un onglet"}
	if !caps.Mouse {
		mouse.feature = "défilement par pgup/pgdn"
	}

	colour := diagRow{name: "couleur", value: caps.Colour.String(), feature: "la palette exactement comme dessinée"}
	if caps.Colour != ColourTrue {
		colour.feature = "la palette ramenée à la couleur la plus proche"
	}

	return []diagRow{graphics, links, mouse, colour}
}

func yesNo(b bool) string {
	if b {
		return "oui"
	}
	return "non"
}

const diagValueWidth = 12

func diagnosticsView(caps Capabilities, styles uiStyles) string {
	var builder strings.Builder
	builder.WriteString(styles.dim.Render("terminal") + "  " + styles.text.Render(caps.Terminal))
	for _, row := range diagRows(caps) {
		pad := strings.Repeat(" ", max(1, diagValueWidth-len(row.value)))
		builder.WriteString("\n" + styles.dim.Render("  "+row.name+strings.Repeat(" ", max(1, factWidth-len(row.name)))) +
			styles.text.Render(row.value) + pad + styles.dim.Render(row.feature))
	}
	builder.WriteString("\n" + styles.dim.Render("  kitty, ghostty et WezTerm ont tous les rendus ; ailleurs l'image"))
	builder.WriteString("\n" + styles.dim.Render("  est dessinée en demi-blocs et un lien reste du texte sélectionnable."))
	return builder.String()
}

func (m *model) openDiagnostics() { m.diagOpen = true }

func (m *model) diagView() string {
	if !m.diagOpen {
		return ""
	}
	return m.st.dim.Render("capacités · échap fermer") + "\n" + diagnosticsView(m.caps, m.st) +
		"\n" + m.remoteImageRow()
}

func (m *model) remoteImageRow() string {
	value, feature := "off", "an image the agent links to stays a link"
	if m.imageFetcher != nil && len(m.imageHosts) > 0 {
		value = "on"
		feature = "images fetched from " + strings.Join(m.imageHosts, ", ")
	}
	name := "remote images"
	return m.st.dim.Render("  "+name+strings.Repeat(" ", max(1, factWidth-len(name)))) +
		m.st.text.Render(value) + strings.Repeat(" ", max(1, diagValueWidth-len(value))) +
		m.st.dim.Render(feature)
}
