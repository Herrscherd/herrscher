package main

import (
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
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

func TestPromptLimitReadsInEachLanguage(t *testing.T) {
	msgs := loadCatalogue()
	units := map[i18n.Language]string{i18n.English: "B", i18n.French: "o"}
	for lang, unit := range units {
		want := clitext.FormatNumber(lang, float64(maxPromptBytes), 0) + " " + unit
		translator := msgs.For(lang)
		got := translator.Text(msgErrPromptLong, params{paramRun: "x", paramLimit: byteSize(maxPromptBytes)})
		if err := translator.Err(); err != nil {
			t.Fatalf("%v: %v", lang, err)
		}
		if !strings.Contains(got, want) || strings.Contains(got, "131071") {
			t.Errorf("%v reads %q, want the limit as %q and no raw count", lang, got, want)
		}
	}
}

func TestBudgetLineShowsOnlyTheLimitsSet(t *testing.T) {
	msgs := loadCatalogue()
	for _, lang := range []i18n.Language{i18n.English, i18n.French} {
		translator := msgs.For(lang)
		tokenUnit := strings.TrimPrefix(translator.Text(msgAmountTokens, params{paramTokens: "X"}), "X")
		moneyUnit := strings.TrimPrefix(translator.Text(msgAmountMoney, params{paramMoney: "X"}), "X")
		tokensOnly := statusLine(translator, report{Status: statusPartial, NotRun: 2, SpentTokens: 9, LimitTokens: 10})
		if !strings.Contains(tokensOnly, tokenUnit) || strings.Contains(tokensOnly, moneyUnit) {
			t.Errorf("%v tokens-only line %q shows the wrong limits", lang, tokensOnly)
		}
		usdOnly := statusLine(translator, report{Status: statusPartial, NotRun: 2, SpentUSD: 0.25, LimitUSD: 1})
		if !strings.Contains(usdOnly, moneyUnit) || strings.Contains(usdOnly, tokenUnit) {
			t.Errorf("%v usd-only line %q shows the wrong limits", lang, usdOnly)
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
