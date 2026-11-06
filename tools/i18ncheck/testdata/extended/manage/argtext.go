package manage

func arguments(kind string) {
	_ = contracts.Text("Reason", "The session has stopped")
	_ = contracts.Text("Name", "tui.keys.reloaded")
	_ = contracts.Text("Kind", kind)
}
