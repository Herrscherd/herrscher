package issuetrigger

import (
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

type Target struct {
	Repo    contracts.ForgeRepoRef
	Agent   string
	Period  time.Duration
	Filters []Filter
}
