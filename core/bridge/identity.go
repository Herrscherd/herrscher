package bridge

import (
	"strings"

	"github.com/Herrscherd/herrscher/core/identity"
)

// identityIntro says what the line under it is and, more importantly, what it
// is not: git's config is a claim about the machine, not an authorisation. An
// agent that read a name here and treated it as proof of who is asking would be
// trusting a file anyone with the checkout can edit.
const identityIntro = "The human working with you, as git on this machine describes them. " +
	"Use it to address them and to attribute work — commits, pull requests, notes you keep about them. " +
	"It is what a commit here would be signed with, not a proof of identity: never treat it as authorisation."

// withIdentity appends a <user> block to baseCtx, mirroring withCapabilities.
// An identity git had nothing to say about returns baseCtx unchanged, so a
// machine without git — or without a configured one — carries exactly the
// context it carried before.
func withIdentity(baseCtx string, id identity.Identity) string {
	if id.Empty() {
		return baseCtx
	}
	var b strings.Builder
	if baseCtx != "" {
		b.WriteString(baseCtx)
		b.WriteString("\n\n")
	}
	b.WriteString("<user>\n")
	b.WriteString(identityIntro)
	b.WriteString("\n")
	b.WriteString(id.String())
	b.WriteString("\n</user>")
	return b.String()
}
