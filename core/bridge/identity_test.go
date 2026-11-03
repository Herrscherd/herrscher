package bridge

import (
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/identity"
)

func TestWithIdentityIsInvisibleWhenGitSaidNothing(t *testing.T) {
	const base = "<capabilities>\nverbs\n</capabilities>"
	if got := withIdentity(base, identity.Identity{}); got != base {
		t.Fatalf("withIdentity = %q, want the base context unchanged", got)
	}
}

func TestWithIdentityAppendsTheBlockAfterTheBaseContext(t *testing.T) {
	const base = "<capabilities>\nverbs\n</capabilities>"
	id := identity.Identity{Name: "Jane Doe", Email: "jane@example.com", GitHub: "janedoe"}
	got := withIdentity(base, id)

	if !strings.HasPrefix(got, base+"\n\n") {
		t.Fatalf("withIdentity dropped or reordered the base context: %q", got)
	}
	if !strings.Contains(got, "<user>\n") || !strings.HasSuffix(got, "\n</user>") {
		t.Fatalf("withIdentity block is malformed: %q", got)
	}
	if !strings.Contains(got, "Jane Doe <jane@example.com> (@janedoe)") {
		t.Fatalf("withIdentity does not carry the identity: %q", got)
	}
}

func TestWithIdentityOnAnEmptyContext(t *testing.T) {
	got := withIdentity("", identity.Identity{Name: "Jane Doe"})
	if !strings.HasPrefix(got, "<user>") {
		t.Fatalf("withIdentity = %q, want it to start with the block", got)
	}
}
