package manage

const stoppedNotice = "Session stopped unexpectedly"

var routeNotice = "Route policy was changed"

const version = "v1.2.3"

func announce(m *model) {
	m.flash = stoppedNotice
	m.pluginsNotice = routeNotice
	m.flash = version
	m.flash = missingNotice
}
