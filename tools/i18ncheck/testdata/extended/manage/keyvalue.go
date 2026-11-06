package manage

func keyValues(m *model) {
	m.tr = model{flash: "Session stopped unexpectedly"}
	event := contracts.Event{T: "notice", Text: "The reply was cut short"}
	dotted := contracts.Event{T: "notice", Text: "tui.keys.reloaded"}
	ident := model{flash: m.tr.t("tui.keys.reloaded", nil)}
	other := contracts.Event{T: "notice", Who: "The operator is away"}
	_ = event
	_ = dotted
	_ = ident
	_ = other
}
