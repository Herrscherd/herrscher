package main

import (
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
)

func TestByteLimitsReadInEachLanguage(t *testing.T) {
	msgs := loadCatalogue()
	wants := map[i18n.Language]string{i18n.English: "1 MiB", i18n.French: "1 Mio"}
	for lang, want := range wants {
		translator := msgs.For(lang)
		got := translator.Text(msgErrRunOutput, params{paramRun: "x", paramLimit: byteSize(maxOutputBytes)})
		if err := translator.Err(); err != nil {
			t.Fatalf("%v: %v", lang, err)
		}
		if !strings.Contains(got, want) {
			t.Errorf("%v reads %q, want the limit as %q", lang, got, want)
		}
		if strings.Contains(got, "1048576") {
			t.Errorf("%v shows the raw byte count: %q", lang, got)
		}
	}
}

func TestStatusLineSeparatesTheCountFromTheStatus(t *testing.T) {
	msgs := loadCatalogue()
	for _, lang := range []i18n.Language{i18n.English, i18n.French} {
		translator := msgs.For(lang)
		count := translator.Text(msgNotStarted, params{paramCount: 2})
		interrupted := statusLine(translator, report{Status: statusInterrupted, NotRun: 2})
		if !strings.Contains(interrupted, ". "+count) {
			t.Errorf("%v interrupted line %q does not separate the count %q", lang, interrupted, count)
		}
		failed := statusLine(translator, report{Status: statusFailed, NotRun: 2, Failure: "boom"})
		if !strings.Contains(failed, ". "+count) {
			t.Errorf("%v failed line %q does not separate the count %q", lang, failed, count)
		}
		partial := statusLine(translator, report{Status: statusPartial, NotRun: 2, SpentTokens: 9, LimitTokens: 10})
		if !strings.Contains(partial, ". "+translator.Text(msgNotRun, params{paramCount: 2})) {
			t.Errorf("%v partial line %q does not separate the arms not run", lang, partial)
		}
	}
}
