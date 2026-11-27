// Package ciloop holds the policy and the log handling of the session CI loop.
package ciloop

import (
	"regexp"
	"strings"
)

var logTagPattern = regexp.MustCompile(`(?i)ci-log`)

// SanitizeLog returns the CI log as valid UTF-8 with every ci-log fence tag neutralised.
func SanitizeLog(raw []byte) string {
	valid := strings.ToValidUTF8(string(raw), "�")
	return logTagPattern.ReplaceAllString(valid, "ci_log")
}
