// Package ciloop holds the policy and the data handling of the session CI loop.
package ciloop

import (
	"strings"
)

// Payload returns the data block of a CI nudge: the check name, its page and the log. Every
// markup character is escaped, so no fence tag can open or close in any writing.
func Payload(check, page, log string) string {
	block := "check: " + check + "\npage: " + page + "\n" + log
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(strings.ToValidUTF8(block, "�"))
}
