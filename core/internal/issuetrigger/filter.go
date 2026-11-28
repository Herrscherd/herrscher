// Package issuetrigger opens one session per forge issue that matches a configured trigger.
package issuetrigger

import (
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// Filter selects the issues of one repository by a label or by a mention of one login.
type Filter struct {
	key       string
	labels    []string
	mentioned contracts.ForgeLogin
}

// LabelFilter selects the issues that carry label.
func LabelFilter(label string) Filter {
	return Filter{key: "label:" + label, labels: []string{label}}
}

// MentionFilter selects the issues that mention login.
func MentionFilter(login contracts.ForgeLogin) Filter {
	return Filter{key: "mention:" + login.String(), mentioned: login}
}

// Key identifies the filter in the persisted watches of its repository.
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

// Trusted reports whether actor may open sessions: an owner, a member or a collaborator.
func Trusted(actor contracts.ForgeActor) bool {
	switch actor.Association {
	case contracts.AssociationOwner, contracts.AssociationMember, contracts.AssociationCollaborator:
		return true
	case contracts.AssociationNone, contracts.AssociationContributor, contracts.AssociationFirstTimer, contracts.AssociationOther:
		return false
	}
	return false
}
