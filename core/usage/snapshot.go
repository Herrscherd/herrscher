package usage

import (
	"encoding/json"
	"fmt"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// Snapshot is what the daemon answers to the usage verb: the limits of the accounts and
// the spend of one period, both filtered by the account and the vendor asked for.
type Snapshot struct {
	At                 time.Time       `json:"at"`
	Range              Range           `json:"range"`
	Account            AccountKey      `json:"account,omitempty"`
	Vendor             string          `json:"vendor,omitempty"`
	Limits             []AccountLimits `json:"limits"`
	Spend              Spend           `json:"spend"`
	LedgerTruncated    bool            `json:"ledgerTruncated,omitempty"`
	TranscriptsPending bool            `json:"transcriptsPending,omitempty"`
	PricesMissing      bool            `json:"pricesMissing,omitempty"`
}

// Encode writes the snapshot in its wire form.
func Encode(snapshot Snapshot) ([]byte, error) {
	out, err := json.Marshal(toWire(snapshot))
	if err != nil {
		return nil, fmt.Errorf("usage snapshot: %w", err)
	}
	return out, nil
}

// Decode reads a snapshot from its wire form.
func Decode(raw []byte) (Snapshot, error) {
	var wire snapshotWire
	if err := json.Unmarshal(raw, &wire); err != nil {
		return Snapshot{}, fmt.Errorf("usage snapshot: %w", err)
	}
	return fromWire(wire), nil
}

type snapshotWire struct {
	At                 time.Time     `json:"at"`
	Range              Range         `json:"range"`
	Account            AccountKey    `json:"account,omitempty"`
	Vendor             string        `json:"vendor,omitempty"`
	Limits             []accountWire `json:"limits"`
	Spend              Spend         `json:"spend"`
	LedgerTruncated    bool          `json:"ledgerTruncated,omitempty"`
	TranscriptsPending bool          `json:"transcriptsPending,omitempty"`
	PricesMissing      bool          `json:"pricesMissing,omitempty"`
}

type accountWire struct {
	Vendor  string       `json:"vendor"`
	Account string       `json:"account,omitempty"`
	Default bool         `json:"default,omitempty"`
	Status  LimitsStatus `json:"status"`
	ReadAt  time.Time    `json:"readAt"`
	Limits  []limitWire  `json:"limits,omitempty"`
}

type limitWire struct {
	Window      contracts.LimitWindow `json:"window"`
	Scope       string                `json:"scope,omitempty"`
	UsedPercent float64               `json:"usedPercent"`
	ResetsAt    time.Time             `json:"resetsAt"`
	Span        time.Duration         `json:"span"`
}

func toWire(snapshot Snapshot) snapshotWire {
	wire := snapshotWire{At: snapshot.At, Range: snapshot.Range, Account: snapshot.Account, Vendor: snapshot.Vendor, Spend: snapshot.Spend, LedgerTruncated: snapshot.LedgerTruncated, TranscriptsPending: snapshot.TranscriptsPending, PricesMissing: snapshot.PricesMissing}
	for _, account := range snapshot.Limits {
		item := accountWire{Vendor: account.Vendor, Account: account.Account, Default: account.Default, Status: account.Status, ReadAt: account.ReadAt}
		for _, limit := range account.Limits {
			item.Limits = append(item.Limits, limitWire{Window: limit.Window, Scope: limit.Scope, UsedPercent: limit.UsedPercent, ResetsAt: limit.ResetsAt, Span: limit.Span})
		}
		wire.Limits = append(wire.Limits, item)
	}
	return wire
}

func fromWire(wire snapshotWire) Snapshot {
	snapshot := Snapshot{At: wire.At, Range: wire.Range, Account: wire.Account, Vendor: wire.Vendor, Spend: wire.Spend, LedgerTruncated: wire.LedgerTruncated, TranscriptsPending: wire.TranscriptsPending, PricesMissing: wire.PricesMissing}
	for _, item := range wire.Limits {
		account := AccountLimits{Vendor: item.Vendor, Account: item.Account, Default: item.Default, Status: item.Status, ReadAt: item.ReadAt}
		for _, limit := range item.Limits {
			account.Limits = append(account.Limits, contracts.Limit{Window: limit.Window, Scope: limit.Scope, UsedPercent: limit.UsedPercent, ResetsAt: limit.ResetsAt, Span: limit.Span})
		}
		snapshot.Limits = append(snapshot.Limits, account)
	}
	return snapshot
}
