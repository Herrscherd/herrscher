package issuetrigger

import (
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// Target is one repository with the filters that open sessions for it, read every Period.
type Target struct {
	Repo    contracts.ForgeRepoRef
	Agent   string
	Period  time.Duration
	Filters []Filter
}
