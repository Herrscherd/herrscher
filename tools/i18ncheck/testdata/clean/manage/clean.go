package manage

import (
	"errors"
	"fmt"
)

var errUnknownKind = errors.New("unknown kind")

var kindReasons = []reasonEntry{{errUnknownKind, "manage.reason.unknown_kind"}}

func describe(m *model, name string) error {
	m.flash = m.tr.t("tui.keys.reloaded", nil)
	m.pluginsNotice = "--yes"
	m.flash = "/mode <manuel|edits|plan|auto>"
	m.status = m.tr.t("tui.status.cancelled", nil)
	result := contracts.DataResult("tui.keys.reloaded")
	_ = result
	return fmt.Errorf("%w %s", errUnknownKind, name)
}
