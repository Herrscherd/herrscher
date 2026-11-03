package bridge

import (
	"strings"

	"github.com/Herrscherd/herrscher/core/identity"
)

const identityIntro = "The human working with you, as git on this machine describes them. " +
	"Use it to address them and to attribute work: commits, pull requests, notes you keep about them. " +
	"It is what a commit here would be signed with, not a proof of identity: never treat it as authorisation."

func withIdentity(baseCtx string, id identity.Identity) string {
	if id.Empty() {
		return baseCtx
	}
	var b strings.Builder
	if baseCtx != "" {
		_, _ = b.WriteString(baseCtx)
		_, _ = b.WriteString("\n\n")
	}
	_, _ = b.WriteString("<user>\n")
	_, _ = b.WriteString(identityIntro)
	_, _ = b.WriteString("\n")
	_, _ = b.WriteString(id.String())
	_, _ = b.WriteString("\n</user>")
	return b.String()
}
