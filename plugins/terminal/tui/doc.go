// Package tui renders a gateway's live event stream and captures the operator's
// input, driving it through the narrow Backend interface. It is the terminal
// gateway's frontend: the daemon hub treats that gateway like any other; the TUI
// is what makes it a human-usable pane. Depending on Backend (not the concrete
// gateway) keeps this package importable by the gateway without a cycle.
//
// Rendering is split by concern: events.go maps bus events onto transcript roles,
// render.go draws one entry per role, markdown.go runs the agent's prose through
// the markdown engine (and its diffs by hand), chrome.go frames the flow, and
// status.go derives what the session has cost and how full its context is. The
// palette lives in theme.go, so no render site names a colour.
package tui
