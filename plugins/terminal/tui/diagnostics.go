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
	default:
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

func diagnosticsView(caps Capabilities) string {
	var b strings.Builder
	b.WriteString(dimStyle.Render("terminal") + "  " + textStyle.Render(caps.Terminal))
	for _, r := range diagRows(caps) {
		pad := strings.Repeat(" ", max(1, diagValueWidth-len(r.value)))
		b.WriteString("\n" + dimStyle.Render("  "+r.name+strings.Repeat(" ", max(1, factWidth-len(r.name)))) +
			textStyle.Render(r.value) + pad + dimStyle.Render(r.feature))
	}
	b.WriteString("\n" + dimStyle.Render("  kitty, ghostty et WezTerm ont tous les rendus ; ailleurs l'image"))
	b.WriteString("\n" + dimStyle.Render("  est dessinée en demi-blocs et un lien reste du texte sélectionnable."))
	return b.String()
}

func (m *model) openDiagnostics() { m.diagOpen = true }

func (m *model) diagView() string {
	if !m.diagOpen {
		return ""
	}
	return dimStyle.Render("capacités · échap fermer") + "\n" + diagnosticsView(m.caps) +
		"\n" + m.remoteImageRow()
}

func (m *model) remoteImageRow() string {
	value, feature := "off", "an image the agent links to stays a link"
	if m.imageFetcher != nil && len(m.imageHosts) > 0 {
		value = "on"
		feature = "images fetched from " + strings.Join(m.imageHosts, ", ")
	}
	name := "remote images"
	return dimStyle.Render("  "+name+strings.Repeat(" ", max(1, factWidth-len(name)))) +
		textStyle.Render(value) + strings.Repeat(" ", max(1, diagValueWidth-len(value))) +
		dimStyle.Render(feature)
}
