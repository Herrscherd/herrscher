package bridge

import (
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/identity"
)

func TestIdentityBlockIsEmptyWhenGitSaidNothing(t *testing.T) {
	if got := identityBlock(identity.Identity{}); got != "" {
		t.Fatalf("identityBlock = %q, want no block", got)
	}
}

func TestIdentityBlockCarriesTheIdentity(t *testing.T) {
	id := identity.Identity{Name: "Jane Doe", Email: "jane@example.com", GitHub: "janedoe"}
	got := identityBlock(id)
	if !strings.HasPrefix(got, "<user>\n") || !strings.HasSuffix(got, "\n</user>") {
		t.Fatalf("identityBlock is malformed: %q", got)
	}
	if !strings.Contains(got, "Jane Doe <jane@example.com> (@janedoe)") {
		t.Fatalf("identityBlock does not carry the identity: %q", got)
	}
}
