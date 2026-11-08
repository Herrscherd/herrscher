package usage

import (
	"errors"
	"fmt"
	"time"
)

// ErrUnknownValue reports a usage enum value or name that does not exist.
var ErrUnknownValue = errors.New("unknown usage value")

// Range is the period a usage report covers, counted back from now.
type Range int

// The periods a usage report can cover.
const (
	Range24h Range = iota + 1
	Range7d
	Range30d
	Range90d
)

const day = 24 * time.Hour

const (
	daysWeek    = 7
	daysMonth   = 30
	daysQuarter = 90
)

func rangeNames() map[Range]string {
	return map[Range]string{
		Range24h: "24h",
		Range7d:  "7d",
		Range30d: "30d",
		Range90d: "90d",
	}
}

// Ranges lists every period, shortest first.
func Ranges() []Range {
	return []Range{Range24h, Range7d, Range30d, Range90d}
}

// DefaultRange is the period a report covers when none is asked.
func DefaultRange() Range {
	return Range7d
}

// Span is the length of the period.
func (r Range) Span() time.Duration {
	switch r {
	case Range24h:
		return day
	case Range7d:
		return daysWeek * day
	case Range30d:
		return daysMonth * day
	case Range90d:
		return daysQuarter * day
	}
	return daysWeek * day
}

// String is the name of the period, as the flag takes it.
func (r Range) String() string {
	return rangeNames()[r]
}

// ParseRange reads the name of a period.
func ParseRange(text string) (Range, error) {
	value, ok := valueOf(text, rangeNames())
	if !ok {
		return 0, fmt.Errorf("range %q: %w", text, ErrUnknownValue)
	}
	return value, nil
}

// MarshalText writes the name of the period.
func (r Range) MarshalText() ([]byte, error) {
	return marshalText("range", r, rangeNames())
}

// UnmarshalText reads the name of the period.
func (r *Range) UnmarshalText(text []byte) error {
	return unmarshalText("range", text, rangeNames(), r)
}
