package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
)

func TestSummarizeDerivesOnOffDeltas(t *testing.T) {
	base := outcome{section: msgSectionPairs, group: msgVariantStale, setting: "report", turns: 2}
	off := func(success bool) outcome {
		o := base
		o.success = success
		o.tokens = 200
		o.input = 180
		return o
	}
	on := func(success bool) outcome {
		o := base
		o.on = true
		o.success = success
		o.tokens = 300
		o.input = 260
		return o
	}
	ex := execution{outcomes: []outcome{off(false), off(true), on(true), on(true)}}
	rep := summarize(config{model: "m", budget: budget{tokens: 1000}}, ex, spend{tokens: 1000}, 4)
	if rep.Status != statusComplete || rep.NotRun != 0 || len(rep.Rows) != 1 {
		t.Fatalf("status %q, not run %d, rows %d; want complete, 0, 1", rep.Status, rep.NotRun, len(rep.Rows))
	}
	r := rep.Rows[0]
	if r.DeltaSuccess == nil || *r.DeltaSuccess != 0.5 {
		t.Errorf("delta success %v, want 0.5 (off 1 of 2, on 2 of 2)", r.DeltaSuccess)
	}
	if r.MemoryTokensPerTurn == nil || *r.MemoryTokensPerTurn != 40 {
		t.Errorf("memory tokens per turn %v, want 40 (on 130, off 90)", r.MemoryTokensPerTurn)
	}
	if r.DeltaTokensPerSuccess == nil || *r.DeltaTokensPerSuccess != -100 {
		t.Errorf("delta tokens per success %v, want -100 (on 300, off 400)", r.DeltaTokensPerSuccess)
	}
}

func TestSummarizeCountsArmsNotRunAfterAStop(t *testing.T) {
	ex := execution{stopped: true, outcomes: []outcome{{section: msgSectionPairs, group: msgVariantSameAgent, setting: "report"}}}
	rep := summarize(config{budget: budget{tokens: 10}}, ex, spend{tokens: 9}, 3)
	if rep.Status != statusPartial || rep.NotRun != 2 || rep.SpentTokens != 9 {
		t.Errorf("status %q, not run %d, spent %d; want partial, 2, 9", rep.Status, rep.NotRun, rep.SpentTokens)
	}
}

func TestWriteReportJSONCarriesTheDeltas(t *testing.T) {
	ex := execution{outcomes: []outcome{
		{section: msgSectionPairs, group: msgVariantSameAgent, setting: "report", success: false, turns: 1, tokens: 50, input: 40},
		{section: msgSectionPairs, group: msgVariantSameAgent, setting: "report", on: true, success: true, turns: 1, tokens: 50, input: 40},
	}}
	rep := summarize(config{budget: budget{tokens: 100}}, ex, spend{tokens: 100}, 2)
	var out bytes.Buffer
	if err := writeReport(&out, rep, config{format: formatJSON}, loadCatalogue()); err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Status string `json:"status"`
		Rows   []struct {
			DeltaSuccess *float64 `json:"deltaSuccess"`
			On           struct {
				Successes int `json:"successes"`
			} `json:"on"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Status != statusComplete || len(decoded.Rows) != 1 || decoded.Rows[0].On.Successes != 1 || decoded.Rows[0].DeltaSuccess == nil || *decoded.Rows[0].DeltaSuccess != 1 {
		t.Errorf("decoded %+v; want complete, one row, one on success, delta 1", decoded)
	}
}

func TestFailedReportKeepsTheArmsFinishedBeforeTheError(t *testing.T) {
	ex := execution{stopped: true, outcomes: []outcome{{section: msgSectionPairs, group: msgVariantSameAgent, setting: "report"}}}
	rep := summarize(config{budget: budget{tokens: 100}}, ex, spend{}, 3)
	rep.Status = statusFailed
	rep.Failure = "run same-agent wrote no usage record"
	var out bytes.Buffer
	if err := writeReport(&out, rep, config{format: formatJSON}, loadCatalogue()); err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Status  string `json:"status"`
		Failure string `json:"failure"`
		NotRun  int    `json:"notRun"`
		Rows    []any  `json:"rows"`
	}
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Status != statusFailed || decoded.Failure != rep.Failure || decoded.NotRun != 2 || len(decoded.Rows) != 1 {
		t.Errorf("decoded %+v; want failed, the failure text, two arms not run, one row kept", decoded)
	}
}

func TestMarkdownHidesFalseRecallForLongMemEval(t *testing.T) {
	ex := execution{outcomes: []outcome{
		{section: msgSectionPairs, group: msgVariantSameAgent, setting: "report"},
		{section: msgSectionLongMemEval, group: msgCapExtraction, setting: "report"},
	}}
	rep := summarize(config{budget: budget{tokens: 100}}, ex, spend{}, 2)
	var out bytes.Buffer
	if err := renderMarkdown(&out, rep, loadCatalogue().For(i18n.English)); err != nil {
		t.Fatal(err)
	}
	var separators []int
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.HasPrefix(line, "|---") {
			separators = append(separators, strings.Count(line, "---|"))
		}
	}
	if len(separators) != 2 || separators[0] != 9 || separators[1] != 8 {
		t.Errorf("table column counts %v, want the pair table with 9 and the LongMemEval table with 8", separators)
	}
}

func TestJSONLeavesLongMemEvalFalseRecallAbsentAndUnpairedDeltasNil(t *testing.T) {
	ex := execution{outcomes: []outcome{
		{section: msgSectionLongMemEval, group: msgCapExtraction, setting: "report", on: true, success: true, turns: 1, tokens: 50, input: 40},
		{section: msgSectionPairs, group: msgVariantSameAgent, setting: "report", on: true, success: true, turns: 1, tokens: 50, input: 40},
	}}
	rep := summarize(config{budget: budget{tokens: 100}}, ex, spend{tokens: 100}, 2)
	var out bytes.Buffer
	if err := writeReport(&out, rep, config{format: formatJSON}, loadCatalogue()); err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Rows []struct {
			Section string `json:"section"`
			Off     struct {
				FalseRecalls *int `json:"falseRecalls"`
			} `json:"off"`
			DeltaSuccess *float64 `json:"deltaSuccess"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Rows) != 2 {
		t.Fatalf("rows %d, want 2", len(decoded.Rows))
	}
	for _, row := range decoded.Rows {
		if row.DeltaSuccess != nil {
			t.Errorf("row %s with no off arm has delta success %v, want null", row.Section, *row.DeltaSuccess)
		}
		if row.Section == string(msgSectionLongMemEval) && row.Off.FalseRecalls != nil {
			t.Errorf("LongMemEval row publishes false recalls %d, want null", *row.Off.FalseRecalls)
		}
	}
}
