package manage

import (
	"regexp"
	"strings"
	"testing"
)

var catalogKeyShape = regexp.MustCompile(`^[a-z]+(\.[a-z_]+)+$`)

func TestSentinelsReadAsEnglishSentences(t *testing.T) {
	sentinels := []error{
		errNoVersions, errNoHostModule, errNotCompiledIn, errNotModulePath,
		errUnknownKind, errInvalidModulePath, errEmptyStack, errUnknownAction,
		errMarkerNotFound, errDuplicateMarker, errMarkerOrder, errNoGatewayCreds,
		errIncompleteCreds, errProbeStatus, errVaultNotDir, errVaultRelative, errInstallAborted, errStepStillRuns, errUnknownSection, errNeedsTerminal,
	}
	for _, sentinel := range sentinels {
		text := sentinel.Error()
		if catalogKeyShape.MatchString(text) || !strings.Contains(text, " ") {
			t.Errorf("sentinel reads as a catalog key, not a sentence: %q", text)
		}
	}
}
