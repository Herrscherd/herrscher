package usage

import (
	"slices"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// ChoiceOutcome tells what a new session starts on.
type ChoiceOutcome uint8

const (
	// ChoiceKept means the planned account stays: it has quota, or its limits are unknown.
	ChoiceKept ChoiceOutcome = iota + 1
	// ChoiceSwitched means the planned account is spent and To takes its place.
	ChoiceSwitched
	// ChoiceNoQuota means the planned account is spent and no account of its vendor has quota.
	ChoiceNoQuota
)

// Choice is the account a new session starts on, with the planned account it replaces.
type Choice struct {
	Outcome ChoiceOutcome
	From    string
	To      string
}

// ChooseAccount picks the account a new session starts on from the limits read
// for every account of one vendor. The planned account moves only when its
// limits are read and a window is at 0 percent remaining. The first ready
// account of the vendor with quota left takes its place, in the order of reads,
// so the caller decides the order. An account whose limits are unavailable or
// failed is neither spent nor a replacement.
func ChooseAccount(planned string, reads []AccountLimits) Choice {
	own := plannedIndex(planned, reads)
	if own < 0 {
		return Choice{Outcome: ChoiceKept, From: planned}
	}
	spent := reads[own]
	if !exhausted(spent) {
		return Choice{Outcome: ChoiceKept, From: spent.Name()}
	}
	for index, candidate := range reads {
		if index == own || candidate.Vendor != spent.Vendor {
			continue
		}
		if hasQuota(candidate) {
			return Choice{Outcome: ChoiceSwitched, From: spent.Name(), To: candidate.Name()}
		}
	}
	return Choice{Outcome: ChoiceNoQuota, From: spent.Name()}
}

// Name is the account name a session records: the native profile for the vendor's own login.
func (a AccountLimits) Name() string {
	if a.Account == "" {
		return contracts.NativeProfile
	}
	return a.Account
}

func plannedIndex(planned string, reads []AccountLimits) int {
	return slices.IndexFunc(reads, func(read AccountLimits) bool {
		if planned == "" {
			return read.Default
		}
		return read.Name() == planned
	})
}

func exhausted(a AccountLimits) bool {
	return a.Status == LimitsReady && windowEmpty(a.Limits)
}

func hasQuota(a AccountLimits) bool {
	return a.Status == LimitsReady && !windowEmpty(a.Limits)
}

func windowEmpty(limits []contracts.Limit) bool {
	return slices.ContainsFunc(limits, func(limit contracts.Limit) bool {
		return Remaining(limit) <= 0
	})
}
