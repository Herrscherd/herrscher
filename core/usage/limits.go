package usage

import (
	"context"
	"errors"
	"sync"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// LimitsTTL is how long the limits of an account are reused before the vendor is asked
// again.
const LimitsTTL = time.Minute

// LimitsFailureTTL is how long a failed reading is reused, shorter than LimitsTTL.
const LimitsFailureTTL = 10 * time.Second

const limitsTimeout = 10 * time.Second

// LimitsStatus tells whether the limits of an account could be read.
type LimitsStatus int

// The outcomes of a limits read.
const (
	LimitsReady LimitsStatus = iota + 1
	LimitsUnavailable
	LimitsFailed
)

func limitsStatusNames() map[LimitsStatus]string {
	return map[LimitsStatus]string{
		LimitsReady:       "ready",
		LimitsUnavailable: "unavailable",
		LimitsFailed:      "failed",
	}
}

// String is the name of the status.
func (s LimitsStatus) String() string {
	return limitsStatusNames()[s]
}

// MarshalText writes the name of the status.
func (s LimitsStatus) MarshalText() ([]byte, error) {
	return marshalText("limits status", s, limitsStatusNames())
}

// UnmarshalText reads the name of the status.
func (s *LimitsStatus) UnmarshalText(text []byte) error {
	return unmarshalText("limits status", text, limitsStatusNames(), s)
}

// Target is one account a vendor can report the limits of.
type Target struct {
	Vendor  string
	Account contracts.ProfileName
	Default bool
	Read    contracts.LimitReader
	Access  contracts.ProbeAccess
}

// AccountLimits is the limits of one account as they were read.
type AccountLimits struct {
	Vendor  string            `json:"vendor"`
	Account string            `json:"account,omitempty"`
	Default bool              `json:"default,omitempty"`
	Status  LimitsStatus      `json:"status"`
	ReadAt  time.Time         `json:"readAt"`
	Limits  []contracts.Limit `json:"limits,omitempty"`
}

// LimitsCache reads the limits of the accounts and reuses each reading for LimitsTTL, or
// for LimitsFailureTTL when the reading failed.
type LimitsCache struct {
	Now     func() time.Time
	Timeout time.Duration
	mu      sync.Mutex
	entries map[AccountKey]AccountLimits
}

type limitsReply struct {
	limits []contracts.Limit
	err    error
}

func (c *LimitsCache) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return limitsTimeout
}

func (e AccountLimits) reusableFor() time.Duration {
	if e.Status == LimitsFailed {
		return LimitsFailureTTL
	}
	return LimitsTTL
}

// Read returns the limits of every target, in the order of the targets. Targets are
// read concurrently, and a reading still fresh is returned without asking the vendor.
func (c *LimitsCache) Read(ctx context.Context, targets []Target) []AccountLimits {
	results := make([]AccountLimits, len(targets))
	var wg sync.WaitGroup
	for index := range targets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[index] = c.readOne(ctx, targets[index])
		}()
	}
	wg.Wait()
	return results
}

func (c *LimitsCache) readOne(ctx context.Context, target Target) AccountLimits {
	key := AccountKey{Vendor: target.Vendor, Account: string(target.Account)}
	now := c.Now()
	c.mu.Lock()
	cached, ok := c.entries[key]
	c.mu.Unlock()
	if ok && now.Sub(cached.ReadAt) < cached.reusableFor() {
		return cached
	}
	readCtx, cancel := context.WithTimeout(ctx, c.timeout())
	defer cancel()
	limits, err := readWithin(readCtx, target)
	entry := AccountLimits{
		Vendor:  target.Vendor,
		Account: string(target.Account),
		Default: target.Default,
		Status:  LimitsReady,
		ReadAt:  now,
		Limits:  limits,
	}
	switch {
	case errors.Is(err, contracts.ErrLimitsUnavailable):
		entry.Status = LimitsUnavailable
		entry.Limits = nil
	case err != nil:
		entry.Status = LimitsFailed
		entry.Limits = nil
	}
	c.mu.Lock()
	if c.entries == nil {
		c.entries = make(map[AccountKey]AccountLimits)
	}
	c.entries[key] = entry
	c.mu.Unlock()
	return entry
}

func readWithin(ctx context.Context, target Target) ([]contracts.Limit, error) {
	reply := make(chan limitsReply, 1)
	go func() {
		limits, err := target.Read(ctx, target.Access)
		reply <- limitsReply{limits: limits, err: err}
	}()
	select {
	case got := <-reply:
		return got.limits, got.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
