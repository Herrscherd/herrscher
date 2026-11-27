package drives

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxNameLength = 32
	maxPathLength = 1024
)

var (
	// ErrNameInvalid reports a drive name outside [a-z0-9][a-z0-9_-]{0,31}.
	ErrNameInvalid = errors.New("a drive name is 1 to 32 lowercase letters, digits, - or _, starting with a letter or a digit")
	// ErrOutsideFolders reports a path that sits in none of the folders the drive exposes.
	ErrOutsideFolders = errors.New("the path sits in none of the folders this drive exposes")
	// ErrPathRequired reports a verb that needs a file path and got the root of the drive.
	ErrPathRequired = errors.New("name a file inside the drive")
)

// InvalidPathError reports a drive path herrscher refuses, with the reason.
type InvalidPathError struct {
	Path   string
	Reason string
}

func (e *InvalidPathError) Error() string {
	return fmt.Sprintf("drive path %q refused: %s", e.Path, e.Reason)
}

// Name is a checked drive name, which is also the name of its rclone remote.
type Name struct {
	value string
}

// ParseName checks a drive name.
func ParseName(raw string) (Name, error) {
	if raw == "" || len(raw) > maxNameLength {
		return Name{}, ErrNameInvalid
	}
	for index, char := range raw {
		lowerOrDigit := (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9')
		if !lowerOrDigit && (index == 0 || (char != '-' && char != '_')) {
			return Name{}, ErrNameInvalid
		}
	}
	return Name{value: raw}, nil
}

func (n Name) String() string { return n.value }

// Path is a checked path inside a drive: relative, slash separated, with no
// empty segment and no . or .. segment. The zero Path is the root of the drive.
type Path struct {
	clean string
}

// ParsePath checks a path inside a drive. An absolute path is refused and a
// trailing slash is dropped.
func ParsePath(raw string) (Path, error) {
	if reason := pathFault(raw); reason != "" {
		return Path{}, &InvalidPathError{Path: raw, Reason: reason}
	}
	return Path{clean: strings.TrimRight(raw, "/")}, nil
}

func pathFault(raw string) string {
	switch {
	case len(raw) > maxPathLength:
		return "longer than 1024 bytes"
	case !utf8.ValidString(raw):
		return "not UTF-8"
	case strings.HasPrefix(raw, "/"):
		return "absolute"
	case strings.ContainsRune(raw, '\\'):
		return "holds a backslash"
	case strings.ContainsFunc(raw, unicode.IsControl):
		return "holds a control character"
	}
	trimmed := strings.TrimRight(raw, "/")
	if trimmed == "" {
		return ""
	}
	for segment := range strings.SplitSeq(trimmed, "/") {
		switch segment {
		case "":
			return "holds an empty segment"
		case ".", "..":
			return "holds a . or .. segment"
		}
	}
	return ""
}

func (p Path) String() string { return p.clean }

// IsRoot reports whether the path is the root of the drive.
func (p Path) IsRoot() bool { return p.clean == "" }

// Within reports whether p is folder or sits under it.
func (p Path) Within(folder Path) bool {
	return folder.IsRoot() || p.clean == folder.clean || strings.HasPrefix(p.clean, folder.clean+"/")
}

// Join appends name, one segment, to p.
func (p Path) Join(name string) (Path, error) {
	if p.IsRoot() {
		return ParsePath(name)
	}
	return ParsePath(p.clean + "/" + name)
}
