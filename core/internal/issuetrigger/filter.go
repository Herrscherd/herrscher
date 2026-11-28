package issuetrigger

import (
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

type Filter struct {
	key       string
	labels    []string
	mentioned contracts.ForgeLogin
}

func LabelFilter(label string) Filter {
	return Filter{key: "label:" + label, labels: []string{label}}
}

func MentionFilter(login contracts.ForgeLogin) Filter {
	return Filter{key: "mention:" + login.String(), mentioned: login}
}

func (f Filter) Key() string { return f.key }

func (f Filter) query(since time.Time) contracts.IssueQuery {
	return contracts.IssueQuery{
		Labels:       f.labels,
		Mentioned:    f.mentioned,
		UpdatedSince: since,
		Sort:         contracts.IssueSortUpdated,
		Direction:    contracts.SortAscending,
	}
}

func Trusted(actor contracts.ForgeActor) bool {
	switch actor.Association {
	case contracts.AssociationOwner, contracts.AssociationMember, contracts.AssociationCollaborator:
		return true
	default:
		return false
	}
}
