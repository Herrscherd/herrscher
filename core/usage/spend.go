package usage

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
)

// AccountKey names one account of a vendor, the native one having an empty name. The
// zero key matches every account.
type AccountKey struct {
	Vendor  string `json:"vendor,omitempty"`
	Account string `json:"account,omitempty"`
}

const nativeName = "native"

// ParseAccountKey reads "vendor:name", the name "native" standing for the native account.
// The empty text is the zero key.
func ParseAccountKey(text string) (AccountKey, error) {
	if text == "" {
		return AccountKey{}, nil
	}
	vendor, name, ok := strings.Cut(text, ":")
	if !ok || vendor == "" || name == "" {
		return AccountKey{}, fmt.Errorf("account %q: %w", text, ErrUnknownValue)
	}
	if name == nativeName {
		name = ""
	}
	return AccountKey{Vendor: vendor, Account: name}, nil
}

// String writes the account the way the usage command takes it, vendor:name, native for the vendor's own account.
func (k AccountKey) String() string {
	if k.Vendor == "" {
		return ""
	}
	name := k.Account
	if name == "" {
		name = nativeName
	}
	return k.Vendor + ":" + name
}

// Matches tells whether the account of a vendor is the one the key names, the zero key naming every account.
func (k AccountKey) Matches(vendor, account string) bool {
	return k.Vendor == "" || (vendor == k.Vendor && account == k.Account)
}

// Spend is what the records of a period cost and carry, in total and split by account
// and by model.
type Spend struct {
	Cost     float64      `json:"cost"`
	Sessions int          `json:"sessions"`
	Tokens   Tokens       `json:"tokens"`
	Buckets  []Bucket     `json:"buckets"`
	Accounts []Share      `json:"accounts,omitempty"`
	Models   []Share      `json:"models,omitempty"`
	Split    CostSplit    `json:"split,omitzero"`
	Saved    float64      `json:"saved,omitempty"`
	Unpriced []string     `json:"unpriced,omitempty"`
	Stages   []StageUsage `json:"stages,omitempty"`
	Jev      JevUsage     `json:"jev"`
}

// Tokens counts the tokens of a period by type.
type Tokens struct {
	In          int64 `json:"in"`
	Out         int64 `json:"out"`
	CacheRead   int64 `json:"cacheRead"`
	CacheCreate int64 `json:"cacheCreate"`
}

// Total is every token of the period, cached or not.
func (t Tokens) Total() int64 {
	return t.In + t.Out + t.CacheRead + t.CacheCreate
}

// Bucket is the cost of one hour, for a 24 h period, or of one day otherwise.
type Bucket struct {
	Start time.Time `json:"start"`
	Cost  float64   `json:"cost"`
}

// Share is the cost and tokens of one account or one model.
type Share struct {
	Vendor  string  `json:"vendor,omitempty"`
	Account string  `json:"account,omitempty"`
	Model   string  `json:"model,omitempty"`
	Cost    float64 `json:"cost"`
	Tokens  int64   `json:"tokens"`
}

// Summarize totals the records of the period that the key matches, counting back from
// now.
func Summarize(records []Record, rng Range, now time.Time, key AccountKey) Spend {
	now = now.UTC()
	step := bucketStep(rng)
	start := bucketStart(now.Add(-rng.Span()), rng)
	count := int(now.Sub(start)/step) + 1
	totals := spendTotals{
		spend:    Spend{Buckets: make([]Bucket, count)},
		start:    start,
		step:     step,
		sessions: make(map[string]bool),
		unpriced: make(map[string]bool),
		accounts: make(map[AccountKey]Share),
		models:   make(map[string]Share),
	}
	for index := range totals.spend.Buckets {
		totals.spend.Buckets[index].Start = start.Add(time.Duration(index) * step)
	}
	for index := range records {
		if records[index].Compaction == nil && key.Matches(records[index].Vendor, records[index].Account) {
			totals.add(&records[index])
		}
	}
	spend := totals.done()
	spend.Stages = stageUsages(records, now.Add(-rng.Span()), key)
	spend.Jev = jevUsages(records, now.Add(-rng.Span()), key)
	return spend
}

type spendTotals struct {
	spend    Spend
	start    time.Time
	step     time.Duration
	sessions map[string]bool
	unpriced map[string]bool
	accounts map[AccountKey]Share
	models   map[string]Share
}

func (t *spendTotals) add(record *Record) {
	spend := &t.spend
	spend.Cost += record.Cost
	spend.Split = spend.Split.plus(record.Split)
	spend.Saved += record.Saved
	spend.Tokens.In += record.In
	spend.Tokens.Out += record.Out
	spend.Tokens.CacheRead += record.CacheRead
	spend.Tokens.CacheCreate += record.CacheCreate
	t.sessions[record.Session] = true
	if record.Unpriced {
		t.unpriced[record.Model] = true
	}
	if at := int(record.Time.UTC().Sub(t.start) / t.step); at >= 0 && at < len(spend.Buckets) {
		spend.Buckets[at].Cost += record.Cost
	}
	account := AccountKey{Vendor: record.Vendor, Account: record.Account}
	accountShare := t.accounts[account]
	accountShare.Vendor, accountShare.Account = record.Vendor, record.Account
	accountShare.Cost += record.Cost
	accountShare.Tokens += tokensOf(*record)
	t.accounts[account] = accountShare
	modelShare := t.models[record.Model]
	modelShare.Model = record.Model
	modelShare.Cost += record.Cost
	modelShare.Tokens += tokensOf(*record)
	t.models[record.Model] = modelShare
}

func (t *spendTotals) done() Spend {
	t.spend.Sessions = len(t.sessions)
	t.spend.Accounts = sortedShares(t.accounts)
	t.spend.Models = sortedShares(t.models)
	t.spend.Unpriced = slices.Sorted(maps.Keys(t.unpriced))
	return t.spend
}

func tokensOf(record Record) int64 {
	return record.In + record.Out + record.CacheRead + record.CacheCreate
}

func sortedShares[K comparable](shares map[K]Share) []Share {
	out := make([]Share, 0, len(shares))
	for _, share := range shares {
		out = append(out, share)
	}
	slices.SortFunc(out, func(left, right Share) int {
		if c := cmp.Compare(right.Cost, left.Cost); c != 0 {
			return c
		}
		return cmp.Compare(left.Vendor+left.Account+left.Model, right.Vendor+right.Account+right.Model)
	})
	return out
}

func bucketStep(rng Range) time.Duration {
	if rng == Range24h {
		return time.Hour
	}
	return day
}

func bucketStart(when time.Time, rng Range) time.Time {
	if rng == Range24h {
		return when.UTC().Truncate(time.Hour)
	}
	return truncDay(when)
}
