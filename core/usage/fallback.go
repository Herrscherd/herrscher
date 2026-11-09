package usage

import (
	"slices"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// ChoiceOutcome tells what a new session starts on.
type ChoiceOutcome uint8

const (
	// ChoiceKept means the planned account stays: it has quota, its limits are unknown, or no replacement is proven.
	ChoiceKept ChoiceOutcome = iota + 1
	// ChoiceSwitched means the planned account is spent and To takes its place.
	ChoiceSwitched
	// ChoiceNoQuota means the planned account is spent and every other account of its vendor is spent too.
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
// limits are read and an account-wide window is at 0 percent remaining. The
// first ready account of the vendor with an account-wide window left takes its
// place, in the order of reads, so the caller decides the order. Without such a
// replacement, the planned account stays: ChoiceNoQuota is reported only when
// every other account of the vendor is read and spent, and an account whose
// limits are unavailable, failed or without a window is never proof of no quota.
func ChooseAccount(planned string, reads []AccountLimits) Choice {
	own := plannedIndex(planned, reads)
	if own < 0 {
		return Choice{Outcome: ChoiceKept, From: planned}
	}
	spent := reads[own]
	if !exhausted(spent) {
		return Choice{Outcome: ChoiceKept, From: spent.Name()}
	}
	allSpent := true
	for index, candidate := range reads {
		if index == own || candidate.Vendor != spent.Vendor {
			continue
		}
		if hasQuota(candidate) {
			return Choice{Outcome: ChoiceSwitched, From: spent.Name(), To: candidate.Name()}
		}
		if !exhausted(candidate) {
			allSpent = false
		}
	}
	if !allSpent {
		return Choice{Outcome: ChoiceKept, From: spent.Name()}
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
	return a.Status == LimitsReady && slices.ContainsFunc(a.Limits, func(limit contracts.Limit) bool {
		return limit.Scope == "" && Remaining(limit) <= 0
	})
}

func hasQuota(a AccountLimits) bool {
	return a.Status == LimitsReady && slices.ContainsFunc(a.Limits, func(limit contracts.Limit) bool {
		return limit.Scope == "" && Remaining(limit) > 0
	})
}
