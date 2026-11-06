package manage

import (
	"errors"
	"fmt"
)

func announce(m *model, count int) error {
	m.flash = "Session stopped unexpectedly"
	m.pluginsNotice = fmt.Sprintf("%d sessions were closed", count)
	result := contracts.DataResult("Nothing was written to disk")
	_ = result
	if count == 0 {
		return errors.New("no session is running here")
	}
	return fmt.Errorf("could not reach the gateway: %w", errGateway)
}
