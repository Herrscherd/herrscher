package usage

import (
	"encoding"
	"fmt"
)

func nameOf[Value comparable](value Value, names map[Value]string) (string, bool) {
	name, ok := names[value]
	return name, ok
}

func valueOf[Value comparable](name string, names map[Value]string) (Value, bool) {
	for value, candidate := range names {
		if candidate == name {
			return value, true
		}
	}
	var zero Value
	return zero, false
}

func marshalText[Value comparable](kind string, value Value, names map[Value]string) ([]byte, error) {
	name, ok := nameOf(value, names)
	if !ok {
		return nil, fmt.Errorf("%s %v: %w", kind, value, ErrUnknownValue)
	}
	return []byte(name), nil
}

func unmarshalText[Value comparable](kind string, text []byte, names map[Value]string, target *Value) error {
	value, ok := valueOf(string(text), names)
	if !ok {
		return fmt.Errorf("%s %q: %w", kind, text, ErrUnknownValue)
	}
	*target = value
	return nil
}

var (
	_ encoding.TextMarshaler   = Range(0)
	_ encoding.TextUnmarshaler = (*Range)(nil)
	_ encoding.TextMarshaler   = LimitsStatus(0)
	_ encoding.TextUnmarshaler = (*LimitsStatus)(nil)
)
