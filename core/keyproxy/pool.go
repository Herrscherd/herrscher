package keyproxy

import (
	"sync"
	"time"

	"github.com/Herrscherd/herrscher/core/config"
)

// PoolOptions are the rules a pool rotates its keys by.
type PoolOptions struct {
	Strategy string
	Statuses []int
	Cooldown time.Duration
	MaxBody  int64
}

// Pool holds one provider keys and the cooldown each one is under.
type Pool struct {
	name     string
	keys     []Secret
	options  PoolOptions
	failures map[int]bool

	mu         sync.Mutex
	cooling    []time.Time
	cursor     int
	lastStatus int
}

// PoolState is what a pool looks like at one instant: how many of its keys are
// usable, and the status that last sent a key to cool down.
type PoolState struct {
	Provider   string
	Keys       int
	Usable     int
	LastStatus int
	CoolingFor time.Duration
}

// NewPool builds a pool over keys with the given rules.
func NewPool(name string, keys []Secret, options PoolOptions) *Pool {
	failures := make(map[int]bool, len(options.Statuses))
	for _, status := range options.Statuses {
		failures[status] = true
	}
	return &Pool{
		name:     name,
		keys:     append([]Secret(nil), keys...),
		options:  options,
		failures: failures,
		cooling:  make([]time.Time, len(keys)),
	}
}

// SingleKeyPool wraps one key that is never rotated, such as the gateway token.
func SingleKeyPool(name string, key Secret) *Pool {
	return NewPool(name, []Secret{key}, PoolOptions{
		Strategy: config.StrategyFailover,
		MaxBody:  config.DefaultMaxBodyBytes,
	})
}

// PoolFromConfig builds a provider pool from its loaded keys and its config.
func PoolFromConfig(name string, keys []Secret, provider config.ProviderKeys) *Pool {
	return NewPool(name, keys, PoolOptions{
		Strategy: provider.EffectiveStrategy(),
		Statuses: provider.EffectiveStatuses(),
		Cooldown: time.Duration(provider.EffectiveCooldownSeconds()) * time.Second,
		MaxBody:  provider.EffectiveMaxBodyBytes(),
	})
}

func (p *Pool) rotates() bool {
	return len(p.keys) > 1
}

func (p *Pool) fails(status int) bool {
	return p.rotates() && p.failures[status]
}

func (p *Pool) pick(now time.Time, tried map[int]bool) (int, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	roundRobin := p.options.Strategy == config.StrategyRoundRobin
	start := 0
	if roundRobin {
		start = p.cursor
	}
	count := len(p.keys)
	for offset := range count {
		index := (start + offset) % count
		if tried[index] || p.cooling[index].After(now) {
			continue
		}
		if roundRobin {
			p.cursor = (index + 1) % count
		}
		return index, true
	}
	return 0, false
}

func (p *Pool) hasRoom(now time.Time, tried map[int]bool) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for index := range p.keys {
		if !tried[index] && !p.cooling[index].After(now) {
			return true
		}
	}
	return false
}

func (p *Pool) cool(index int, now time.Time, retryAfter time.Duration, status int) {
	duration := retryAfter
	if duration <= 0 {
		duration = p.options.Cooldown
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cooling[index] = now.Add(duration)
	p.lastStatus = status
}

// State returns the pool as it stands at now. CoolingFor is how long until the
// next cooling key is usable again, zero when none cools.
func (p *Pool) State(now time.Time) PoolState {
	p.mu.Lock()
	defer p.mu.Unlock()
	state := PoolState{Provider: p.name, Keys: len(p.keys), LastStatus: p.lastStatus}
	for _, until := range p.cooling {
		wait := until.Sub(now)
		if wait <= 0 {
			state.Usable++
			continue
		}
		if state.CoolingFor == 0 || wait < state.CoolingFor {
			state.CoolingFor = wait
		}
	}
	return state
}

func (p *Pool) soonestAvailable(now time.Time) time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	var soonest time.Duration
	for _, until := range p.cooling {
		wait := until.Sub(now)
		if wait <= 0 {
			return 0
		}
		if soonest == 0 || wait < soonest {
			soonest = wait
		}
	}
	return soonest
}

func (p *Pool) key(index int) Secret {
	return p.keys[index]
}
