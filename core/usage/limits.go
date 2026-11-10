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
// for LimitsFailureTTL when the reading failed. Callers asking for one account while it
// is read share that reading, which no caller cancels and which stops at Timeout, ten
// seconds when unset. A reading that ends within Timeout after its callers gave up is
// still kept, so the next caller gets it, while one that runs past Timeout fails. A reader that ignores its context is left to finish, and no
// second reading of that account starts until it does.
type LimitsCache struct {
	Now      func() time.Time
	Timeout  time.Duration
	mu       sync.Mutex
	entries  map[AccountKey]AccountLimits
	inflight map[AccountKey]*pendingRead
}

type pendingRead struct {
	done   chan struct{}
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
	limits, err := c.readWithin(readCtx, key, target)
	if ctx.Err() != nil {
		return AccountLimits{Vendor: target.Vendor, Account: string(target.Account), Default: target.Default, Status: LimitsFailed, ReadAt: now}
	}
	entry := limitsEntry(target, now, limits, err)
	c.store(key, entry)
	return entry
}

func limitsEntry(target Target, readAt time.Time, limits []contracts.Limit, err error) AccountLimits {
	entry := AccountLimits{
		Vendor:  target.Vendor,
		Account: string(target.Account),
		Default: target.Default,
		Status:  LimitsReady,
		ReadAt:  readAt,
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
	return entry
}

func (c *LimitsCache) store(key AccountKey, entry AccountLimits) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[AccountKey]AccountLimits)
	}
	if known, ok := c.entries[key]; ok && known.ReadAt.After(entry.ReadAt) {
		return
	}
	c.entries[key] = entry
}

func (c *LimitsCache) readWithin(ctx context.Context, key AccountKey, target Target) ([]contracts.Limit, error) {
	pending := c.start(ctx, key, target)
	select {
	case <-pending.done:
		return pending.limits, pending.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *LimitsCache) start(ctx context.Context, key AccountKey, target Target) *pendingRead {
	c.mu.Lock()
	defer c.mu.Unlock()
	if pending, ok := c.inflight[key]; ok {
		return pending
	}
	if c.inflight == nil {
		c.inflight = make(map[AccountKey]*pendingRead)
	}
	pending := &pendingRead{done: make(chan struct{})}
	c.inflight[key] = pending
	readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.timeout())
	go func() {
		defer cancel()
		pending.limits, pending.err = target.Read(readCtx, target.Access)
		c.store(key, limitsEntry(target, c.Now(), pending.limits, pending.err))
		c.mu.Lock()
		delete(c.inflight, key)
		c.mu.Unlock()
		close(pending.done)
	}()
	return pending
}
