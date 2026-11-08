package usage

import (
	"math"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const (
	paceMinSpan  = 24 * time.Hour
	paceMinDelta = 10
)

// Pace places a limit against the time of its window: Elapsed is the share of the
// window already gone, Delta how far the usage runs ahead of it, in points.
type Pace struct {
	Elapsed float64
	Delta   float64
	Shown   bool
}

// PaceOf reads the pace of a limit at now. The delta is shown only for a whole-account
// window of at least a day, not exhausted, that the usage runs ahead or behind by ten
// points or more.
func PaceOf(limit contracts.Limit, now time.Time) Pace {
	if limit.Span <= 0 {
		return Pace{}
	}
	left := limit.ResetsAt.Sub(now)
	elapsed := clamp(float64(limit.Span-left) / float64(limit.Span) * percentScale)
	delta := limit.UsedPercent - elapsed
	shown := limit.Scope == "" && limit.Span >= paceMinSpan && Remaining(limit) > 0 && math.Abs(delta) >= paceMinDelta
	return Pace{Elapsed: elapsed, Delta: delta, Shown: shown}
}

// Remaining is the share of the limit still available, from 0 to 100.
func Remaining(limit contracts.Limit) float64 {
	return clamp(percentScale - limit.UsedPercent)
}

// ResetIn is the time left before the window resets, zero once it has.
func ResetIn(limit contracts.Limit, now time.Time) time.Duration {
	if left := limit.ResetsAt.Sub(now); left > 0 {
		return left
	}
	return 0
}

func clamp(value float64) float64 {
	return math.Max(0, math.Min(percentScale, value))
}
