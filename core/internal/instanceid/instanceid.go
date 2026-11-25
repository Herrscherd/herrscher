// Package instanceid resolves and validates the per-daemon instance identifier
// used to namespace global resources (git branches, worktree paths, channel
// titles) so multiple daemons can share one gateway home.
package instanceid

import (
	"crypto/rand"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const (
	maxOwnerSuffix  = 8
	generatedPrefix = "h"
	generatedLength = 8
)

var (
	idRe         = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,15}$`)
	errInvalidID = errors.New("invalid instance id")
)

// ErrUnconfigured reports that neither an explicit id nor an owner was given to derive one from.
var ErrUnconfigured = errors.New("neither HERRSCHER_INSTANCE_ID nor HERRSCHER_OWNER_ID is set")

// Validate reports whether id is a well-formed instanceID slug.
func Validate(id string) bool {
	return idRe.MatchString(id)
}

// Slugify derives a short instanceID from an owner identifier (e.g. a gateway
// account snowflake). It returns
// "u" + the last up-to-8 characters of owner, which keeps the result <=9 chars
// and within the validation regex. An empty owner yields an empty string.
func Slugify(owner string) string {
	if owner == "" {
		return ""
	}
	if len(owner) > maxOwnerSuffix {
		owner = owner[len(owner)-maxOwnerSuffix:]
	}
	return "u" + owner
}

// Resolve computes the instanceID from an explicit id (HERRSCHER_INSTANCE_ID), else from the owner (HERRSCHER_OWNER_ID), else returns ErrUnconfigured.
func Resolve(explicit, owner string) (string, error) {
	if explicit != "" {
		if !Validate(explicit) {
			return "", fmt.Errorf("%w: HERRSCHER_INSTANCE_ID %q, want %s", errInvalidID, explicit, idRe.String())
		}
		return explicit, nil
	}
	if owner == "" {
		return "", ErrUnconfigured
	}
	derived := Slugify(owner)
	if !Validate(derived) {
		return "", fmt.Errorf("%w: HERRSCHER_OWNER_ID %q derives %q", errInvalidID, owner, derived)
	}
	return derived, nil
}

// Generate returns a fresh random instanceID, for a daemon that was given neither an id nor an owner.
func Generate() string {
	return generatedPrefix + strings.ToLower(rand.Text()[:generatedLength])
}
