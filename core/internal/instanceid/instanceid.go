// Package instanceid resolves and validates the per-daemon instance identifier
// used to namespace global resources (git branches, worktree paths, channel
// titles) so multiple daemons can share one gateway home.
package instanceid

import (
	"errors"
	"fmt"
	"regexp"
)

var (
	idRe         = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,15}$`)
	errInvalidID = errors.New("invalid instance id")
)

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
	if len(owner) > 8 {
		owner = owner[len(owner)-8:]
	}
	return "u" + owner
}

// Resolve computes the instanceID from an explicit id (e.g. HERRSCHER_INSTANCE_ID)
// and a fallback owner snowflake (e.g. HERRSCHER_OWNER_ID), per Spec §2:
//  1. explicit, when set, must be a valid slug or Resolve errors;
//  2. otherwise the owner is slugified;
//  3. otherwise "" (legacy, non-namespaced mode).
func Resolve(explicit, owner string) (string, error) {
	if explicit != "" {
		if !Validate(explicit) {
			return "", fmt.Errorf("%w: HERRSCHER_INSTANCE_ID %q, want %s", errInvalidID, explicit, idRe.String())
		}
		return explicit, nil
	}
	derived := Slugify(owner)
	if derived != "" && !Validate(derived) {
		return "", fmt.Errorf("%w: HERRSCHER_OWNER_ID %q derives %q", errInvalidID, owner, derived)
	}
	return derived, nil
}
