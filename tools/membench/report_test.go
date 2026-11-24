package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/Herrscherd/herrscher/core/clitext"
)

func TestSummarizeDerivesOnOffDeltas(t *testing.T) {
	base := outcome{section: clitext.MembenchSectionPairs, group: clitext.MembenchVariantStale, setting: "report", turns: 2}
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
	if r.DeltaSuccess != 0.5 {
		t.Errorf("delta success %v, want 0.5 (off 1 of 2, on 2 of 2)", r.DeltaSuccess)
	}
	if r.MemoryTokensPerTurn != 40 {
		t.Errorf("memory tokens per turn %v, want 40 (on 130, off 90)", r.MemoryTokensPerTurn)
	}
	if r.DeltaTokensPerSuccess == nil || *r.DeltaTokensPerSuccess != -100 {
		t.Errorf("delta tokens per success %v, want -100 (on 300, off 400)", r.DeltaTokensPerSuccess)
	}
}

func TestSummarizeCountsArmsNotRunAfterAStop(t *testing.T) {
	ex := execution{stopped: true, outcomes: []outcome{{section: clitext.MembenchSectionPairs, group: clitext.MembenchVariantSameAgent, setting: "report"}}}
	rep := summarize(config{budget: budget{tokens: 10}}, ex, spend{tokens: 9}, 3)
	if rep.Status != statusPartial || rep.NotRun != 2 || rep.SpentTokens != 9 {
		t.Errorf("status %q, not run %d, spent %d; want partial, 2, 9", rep.Status, rep.NotRun, rep.SpentTokens)
	}
}

func TestWriteReportJSONCarriesTheDeltas(t *testing.T) {
	ex := execution{outcomes: []outcome{{section: clitext.MembenchSectionPairs, group: clitext.MembenchVariantSameAgent, setting: "report", on: true, success: true, turns: 1, tokens: 50, input: 40}}}
	rep := summarize(config{budget: budget{tokens: 100}}, ex, spend{tokens: 50}, 1)
	var out bytes.Buffer
	if err := writeReport(&out, rep, config{format: formatJSON}); err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Status string `json:"status"`
		Rows   []struct {
			DeltaSuccess float64 `json:"deltaSuccess"`
			On           struct {
				Successes int `json:"successes"`
			} `json:"on"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Status != statusComplete || len(decoded.Rows) != 1 || decoded.Rows[0].On.Successes != 1 || decoded.Rows[0].DeltaSuccess != 1 {
		t.Errorf("decoded %+v; want complete, one row, one on success, delta 1", decoded)
	}
}
