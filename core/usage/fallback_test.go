package usage

import (
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func readAccount(vendor, account string, def bool, status LimitsStatus, usedPercent float64) AccountLimits {
	return AccountLimits{
		Vendor:  vendor,
		Account: account,
		Default: def,
		Status:  status,
		Limits:  []contracts.Limit{{UsedPercent: usedPercent}},
	}
}

func readWindows(vendor, account string, status LimitsStatus, limits ...contracts.Limit) AccountLimits {
	return AccountLimits{Vendor: vendor, Account: account, Status: status, Limits: limits}
}

func TestChooseAccount(t *testing.T) {
	cases := map[string]struct {
		planned string
		reads   []AccountLimits
		want    Choice
	}{
		"planned has quota": {
			planned: "work",
			reads: []AccountLimits{
				readAccount("claude", "work", false, LimitsReady, 40),
				readAccount("claude", "perso", false, LimitsReady, 10),
			},
			want: Choice{Outcome: ChoiceKept, From: "work"},
		},
		"planned spent, next account takes over": {
			planned: "work",
			reads: []AccountLimits{
				readAccount("claude", "work", false, LimitsReady, 100),
				readAccount("claude", "perso", false, LimitsReady, 10),
			},
			want: Choice{Outcome: ChoiceSwitched, From: "work", To: "perso"},
		},
		"default planned, native replacement is recorded as native": {
			planned: "",
			reads: []AccountLimits{
				readAccount("claude", "", true, LimitsReady, 100),
				readAccount("claude", "perso", false, LimitsReady, 0),
			},
			want: Choice{Outcome: ChoiceSwitched, From: contracts.NativeProfile, To: "perso"},
		},
		"native account taken over by a profile": {
			planned: contracts.NativeProfile,
			reads: []AccountLimits{
				readAccount("claude", "", false, LimitsReady, 100),
				readAccount("claude", "perso", false, LimitsReady, 20),
			},
			want: Choice{Outcome: ChoiceSwitched, From: contracts.NativeProfile, To: "perso"},
		},
		"planned unavailable is not spent": {
			planned: "work",
			reads: []AccountLimits{
				readAccount("claude", "work", false, LimitsUnavailable, 0),
				readAccount("claude", "perso", false, LimitsReady, 10),
			},
			want: Choice{Outcome: ChoiceKept, From: "work"},
		},
		"planned failed is not spent": {
			planned: "work",
			reads: []AccountLimits{
				readAccount("claude", "work", false, LimitsFailed, 100),
				readAccount("claude", "perso", false, LimitsReady, 10),
			},
			want: Choice{Outcome: ChoiceKept, From: "work"},
		},
		"unavailable candidate is skipped": {
			planned: "work",
			reads: []AccountLimits{
				readAccount("claude", "work", false, LimitsReady, 100),
				readAccount("claude", "ghost", false, LimitsUnavailable, 0),
				readAccount("claude", "perso", false, LimitsReady, 10),
			},
			want: Choice{Outcome: ChoiceSwitched, From: "work", To: "perso"},
		},
		"spent candidate is skipped": {
			planned: "work",
			reads: []AccountLimits{
				readAccount("claude", "work", false, LimitsReady, 100),
				readAccount("claude", "perso", false, LimitsReady, 100),
				readAccount("claude", "third", false, LimitsReady, 50),
			},
			want: Choice{Outcome: ChoiceSwitched, From: "work", To: "third"},
		},
		"first ready candidate in read order wins": {
			planned: "work",
			reads: []AccountLimits{
				readAccount("claude", "work", false, LimitsReady, 100),
				readAccount("claude", "first", false, LimitsReady, 5),
				readAccount("claude", "second", false, LimitsReady, 5),
			},
			want: Choice{Outcome: ChoiceSwitched, From: "work", To: "first"},
		},
		"no account has quota": {
			planned: "work",
			reads: []AccountLimits{
				readAccount("claude", "work", false, LimitsReady, 100),
				readAccount("claude", "perso", false, LimitsReady, 100),
			},
			want: Choice{Outcome: ChoiceNoQuota, From: "work"},
		},
		"other vendors are never a replacement": {
			planned: "work",
			reads: []AccountLimits{
				readAccount("claude", "work", false, LimitsReady, 100),
				readAccount("codex", "perso", false, LimitsReady, 0),
			},
			want: Choice{Outcome: ChoiceNoQuota, From: "work"},
		},
		"unknown planned account keeps the planned name": {
			planned: "ghost",
			reads: []AccountLimits{
				readAccount("claude", "perso", false, LimitsReady, 0),
			},
			want: Choice{Outcome: ChoiceKept, From: "ghost"},
		},
		"unavailable candidate blocks the no-quota notice": {
			planned: "work",
			reads: []AccountLimits{
				readAccount("claude", "work", false, LimitsReady, 100),
				readAccount("claude", "ghost", false, LimitsUnavailable, 0),
			},
			want: Choice{Outcome: ChoiceKept, From: "work"},
		},
		"failed candidate blocks the no-quota notice": {
			planned: "work",
			reads: []AccountLimits{
				readAccount("claude", "work", false, LimitsReady, 100),
				readAccount("claude", "perso", false, LimitsFailed, 100),
			},
			want: Choice{Outcome: ChoiceKept, From: "work"},
		},
		"ready candidate without any window is no replacement": {
			planned: "work",
			reads: []AccountLimits{
				readAccount("claude", "work", false, LimitsReady, 100),
				{Vendor: "claude", Account: "perso", Status: LimitsReady},
			},
			want: Choice{Outcome: ChoiceKept, From: "work"},
		},
		"ready candidate with only scoped windows is no replacement": {
			planned: "work",
			reads: []AccountLimits{
				readAccount("claude", "work", false, LimitsReady, 100),
				readWindows("claude", "perso", LimitsReady, contracts.Limit{Scope: "opus", UsedPercent: 0}),
			},
			want: Choice{Outcome: ChoiceKept, From: "work"},
		},
		"scoped window at 0 percent does not spend the planned account": {
			planned: "work",
			reads: []AccountLimits{
				readWindows("claude", "work", LimitsReady, contracts.Limit{Scope: "opus", UsedPercent: 100}, contracts.Limit{UsedPercent: 20}),
				readAccount("claude", "perso", false, LimitsReady, 10),
			},
			want: Choice{Outcome: ChoiceKept, From: "work"},
		},
		"scoped window at 0 percent on a candidate does not block it": {
			planned: "work",
			reads: []AccountLimits{
				readAccount("claude", "work", false, LimitsReady, 100),
				readWindows("claude", "perso", LimitsReady, contracts.Limit{Scope: "opus", UsedPercent: 100}, contracts.Limit{UsedPercent: 10}),
			},
			want: Choice{Outcome: ChoiceSwitched, From: "work", To: "perso"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := ChooseAccount(tc.planned, tc.reads); got != tc.want {
				t.Fatalf("ChooseAccount(%q) = %+v, want %+v", tc.planned, got, tc.want)
			}
		})
	}
}
