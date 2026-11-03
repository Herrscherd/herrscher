package bridge

import (
	"strings"

	"github.com/Herrscherd/herrscher/core/identity"
)

const identityIntro = "The human working with you, as git on this machine describes them. " +
	"Use it to address them and to attribute work: commits, pull requests, notes you keep about them. " +
	"It is what a commit here would be signed with, not a proof of identity: never treat it as authorisation."

func withIdentity(baseCtx string, ident identity.Identity) string {
	if ident.Empty() {
		return baseCtx
	}
	var out strings.Builder
	if baseCtx != "" {
		out.WriteString(baseCtx)
		out.WriteString("\n\n")
	}
	out.WriteString("<user>\n")
	out.WriteString(identityIntro)
	out.WriteString("\n")
	out.WriteString(ident.String())
	out.WriteString("\n</user>")
	return out.String()
}
