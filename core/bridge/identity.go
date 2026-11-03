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
		_, _ = out.WriteString(baseCtx)
		_, _ = out.WriteString("\n\n")
	}
	_, _ = out.WriteString("<user>\n")
	_, _ = out.WriteString(identityIntro)
	_, _ = out.WriteString("\n")
	_, _ = out.WriteString(ident.String())
	_, _ = out.WriteString("\n</user>")
	return out.String()
}
