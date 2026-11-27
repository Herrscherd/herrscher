// Package drives keeps the external drives a herrscher daemon exposes to its
// agents: their catalogue, the access each agent holds, the rclone remotes
// behind them and the journal of what each session read and wrote.
package drives

import "fmt"

// Access is what an agent may do on a drive.
type Access int

const (
	// AccessNone hides the drive from the agent.
	AccessNone Access = iota
	// AccessRead lets the agent list and read inside the exposed folders.
	AccessRead
	// AccessWrite also lets the agent ask to write a file, each write put to the operator.
	AccessWrite
)

const (
	accessNoneName  = "none"
	accessReadName  = "read"
	accessWriteName = "write"
)

// UnknownAccessError reports an access level that is none of none, read and write.
type UnknownAccessError struct {
	Value string
}

func (e *UnknownAccessError) Error() string {
	return fmt.Sprintf("drive access %q is none of none, read and write", e.Value)
}

// ParseAccess reads an access level as an operator writes it.
func ParseAccess(raw string) (Access, error) {
	switch raw {
	case accessNoneName:
		return AccessNone, nil
	case accessReadName:
		return AccessRead, nil
	case accessWriteName:
		return AccessWrite, nil
	}
	return AccessNone, &UnknownAccessError{Value: raw}
}

func (a Access) String() string {
	switch a {
	case AccessNone:
		return accessNoneName
	case AccessRead:
		return accessReadName
	case AccessWrite:
		return accessWriteName
	}
	return fmt.Sprintf("access(%d)", int(a))
}

// Allows reports whether holding a covers the need.
func (a Access) Allows(need Access) bool {
	return a >= need
}

// MarshalText writes the access as its name.
func (a Access) MarshalText() ([]byte, error) {
	return []byte(a.String()), nil
}

// UnmarshalText reads the access from its name.
func (a *Access) UnmarshalText(text []byte) error {
	parsed, err := ParseAccess(string(text))
	if err != nil {
		return err
	}
	*a = parsed
	return nil
}
