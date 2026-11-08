package health

import (
	"errors"
	"fmt"
)

// Group is the family a check belongs to. It is a closed set whose zero value is
// not a group, and the order of [Groups] is the order a report lists them in.
type Group int

const (
	// GroupCore holds the daemon itself and its key proxy.
	GroupCore Group = iota + 1
	// GroupGateways holds the gateway plugins.
	GroupGateways
	// GroupBackends holds every backend plugin for every account.
	GroupBackends
	// GroupCompaction holds the stages of the compaction stack.
	GroupCompaction
	// GroupMemory holds the memory plugins.
	GroupMemory
	// GroupKeys holds the key pools of the key proxy.
	GroupKeys
)

// ErrUnknownGroup is returned for text, or a value, that names no group. Match
// it with errors.Is.
var ErrUnknownGroup = errors.New("unknown health group")

// Groups lists every group, in report order.
func Groups() []Group {
	return []Group{GroupCore, GroupGateways, GroupBackends, GroupCompaction, GroupMemory, GroupKeys}
}

// String renders the group as its text form, the one [ParseGroup] reads back.
func (g Group) String() string {
	switch g {
	case GroupCore:
		return "core"
	case GroupGateways:
		return "gateways"
	case GroupBackends:
		return "backends"
	case GroupCompaction:
		return "compaction"
	case GroupMemory:
		return "memory"
	case GroupKeys:
		return "keys"
	}
	return fmt.Sprintf("Group(%d)", int(g))
}

// ParseGroup reads a group from the text form [Group.String] writes.
func ParseGroup(text string) (Group, error) {
	for _, group := range Groups() {
		if group.String() == text {
			return group, nil
		}
	}
	return 0, fmt.Errorf("%w: %q", ErrUnknownGroup, text)
}

// MarshalText writes the text form of the group, and refuses a value that names
// no group.
func (g Group) MarshalText() ([]byte, error) {
	if _, err := ParseGroup(g.String()); err != nil {
		return nil, err
	}
	return []byte(g.String()), nil
}

// UnmarshalText reads the text form of a group, as [ParseGroup] does.
func (g *Group) UnmarshalText(text []byte) error {
	parsed, err := ParseGroup(string(text))
	if err != nil {
		return err
	}
	*g = parsed
	return nil
}
