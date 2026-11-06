package manage

func joined(m *model, name string) {
	m.flash = "Session " + name + " was stopped"
	m.pluginsNotice = "Count: " + name
	m.flash = "-" + name
}
