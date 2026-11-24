package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
)

const (
	statusComplete = "complete"
	statusPartial  = "partial"
	signedPrefix   = "+"
	percent        = 100
	turnDecimals   = 2
	memoryDecimals = 1
	moneyDecimals  = 4
	jsonIndent     = "  "
)

type cell struct {
	Runs         int   `json:"runs"`
	Successes    int   `json:"successes"`
	FalseRecalls int   `json:"falseRecalls"`
	Turns        int   `json:"turns"`
	Tokens       int64 `json:"tokens"`
	Input        int64 `json:"input"`
}

func (c *cell) add(got outcome) {
	c.Runs++
	c.Turns += got.turns
	c.Tokens += got.tokens
	c.Input += got.input
	if got.success {
		c.Successes++
	}
	if got.falseRecall {
		c.FalseRecalls++
	}
}

func (c *cell) successRate() float64 {
	if c.Runs == 0 {
		return 0
	}
	return float64(c.Successes) / float64(c.Runs)
}

func (c *cell) averageTurns() float64 {
	if c.Runs == 0 {
		return 0
	}
	return float64(c.Turns) / float64(c.Runs)
}

func (c *cell) inputPerTurn() float64 {
	if c.Turns == 0 {
		return 0
	}
	return float64(c.Input) / float64(c.Turns)
}

func (c *cell) tokensPerSuccess() (float64, bool) {
	if c.Successes == 0 {
		return 0, false
	}
	return float64(c.Tokens) / float64(c.Successes), true
}

type row struct {
	Section               clitext.Key `json:"section"`
	Group                 clitext.Key `json:"group"`
	Setting               string      `json:"setting"`
	Off                   cell        `json:"off"`
	On                    cell        `json:"on"`
	DeltaSuccess          float64     `json:"deltaSuccess"`
	DeltaTurns            float64     `json:"deltaTurns"`
	DeltaTokensPerSuccess *float64    `json:"deltaTokensPerSuccess"`
	MemoryTokensPerTurn   float64     `json:"memoryTokensPerTurn"`
}

func (r *row) derive() {
	r.DeltaSuccess = r.On.successRate() - r.Off.successRate()
	r.DeltaTurns = r.On.averageTurns() - r.Off.averageTurns()
	r.MemoryTokensPerTurn = r.On.inputPerTurn() - r.Off.inputPerTurn()
	onTokens, onMeasured := r.On.tokensPerSuccess()
	offTokens, offMeasured := r.Off.tokensPerSuccess()
	if onMeasured && offMeasured {
		delta := onTokens - offTokens
		r.DeltaTokensPerSuccess = &delta
	}
}

type report struct {
	Status      string  `json:"status"`
	Model       string  `json:"model"`
	LimitTokens int64   `json:"limitTokens"`
	LimitUSD    float64 `json:"limitUsd"`
	SpentTokens int64   `json:"spentTokens"`
	SpentUSD    float64 `json:"spentUsd"`
	NotRun      int     `json:"notRun"`
	Rows        []row   `json:"rows"`
}

type rowKey struct {
	section clitext.Key
	group   clitext.Key
	setting string
}

func summarize(cfg config, executed execution, spent spend, planned int) report {
	rep := report{
		Status:      statusComplete,
		Model:       cfg.model,
		LimitTokens: cfg.budget.tokens,
		LimitUSD:    cfg.budget.usd,
		SpentTokens: spent.tokens,
		SpentUSD:    spent.usd,
		NotRun:      planned - len(executed.outcomes),
	}
	if executed.stopped {
		rep.Status = statusPartial
	}
	index := make(map[rowKey]int)
	for i := range executed.outcomes {
		got := executed.outcomes[i]
		key := rowKey{section: got.section, group: got.group, setting: got.setting}
		position, seen := index[key]
		if !seen {
			rep.Rows = append(rep.Rows, row{Section: got.section, Group: got.group, Setting: got.setting})
			position = len(rep.Rows) - 1
			index[key] = position
		}
		if got.on {
			rep.Rows[position].On.add(got)
		} else {
			rep.Rows[position].Off.add(got)
		}
	}
	for i := range rep.Rows {
		rep.Rows[i].derive()
	}
	return rep
}

func writeReport(out io.Writer, rep report, cfg config) error {
	if cfg.format == formatJSON {
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", jsonIndent)
		return encoder.Encode(rep)
	}
	return renderMarkdown(out, rep, cfg.lang)
}

func renderMarkdown(out io.Writer, rep report, lang i18n.Language) error {
	translator := clitext.NewMessages().For(lang)
	var text strings.Builder
	text.WriteString("# " + translator.Text(clitext.MembenchReportTitle, nil) + "\n\n")
	text.WriteString(statusLine(translator, rep, lang) + "\n")
	for _, section := range []clitext.Key{clitext.MembenchSectionPairs, clitext.MembenchSectionLongMemEval} {
		rows := rowsOf(rep.Rows, section)
		if len(rows) == 0 {
			continue
		}
		text.WriteString("\n## " + translator.Text(section, nil) + "\n\n")
		text.WriteString(table(translator, section, rows, lang))
	}
	if len(rep.Rows) == 0 {
		text.WriteString("\n" + translator.Text(clitext.MembenchNoMeasure, nil) + "\n")
	}
	if err := translator.Err(); err != nil {
		return err
	}
	_, err := io.WriteString(out, text.String())
	return err
}

func statusLine(translator *clitext.Translator, rep report, lang i18n.Language) string {
	if rep.Status != statusPartial {
		return translator.Text(clitext.MembenchStatusComplete, nil)
	}
	stopped := translator.Text(clitext.MembenchBudgetStopped, clitext.Params{
		clitext.ParamSpent: amountText(translator, lang, rep.SpentTokens, rep.SpentUSD),
		clitext.ParamLimit: amountText(translator, lang, rep.LimitTokens, rep.LimitUSD),
	})
	notRun := translator.Text(clitext.MembenchNotRun, clitext.Params{clitext.ParamCount: clitext.FormatNumber(lang, float64(rep.NotRun), 0)})
	return translator.Text(clitext.MembenchStatusPartial, nil) + " " + stopped + " " + notRun
}

func amountText(translator *clitext.Translator, lang i18n.Language, tokens int64, usd float64) string {
	return translator.Text(clitext.MembenchAmount, clitext.Params{
		clitext.ParamTokens: clitext.FormatNumber(lang, float64(tokens), 0),
		clitext.ParamMoney:  clitext.FormatNumber(lang, usd, moneyDecimals),
	})
}

func rowsOf(rows []row, section clitext.Key) []row {
	var matched []row
	for index := range rows {
		if rows[index].Section == section {
			matched = append(matched, rows[index])
		}
	}
	return matched
}

func table(translator *clitext.Translator, section clitext.Key, rows []row, lang i18n.Language) string {
	headers := []clitext.Key{
		firstColumn(section), clitext.MembenchColSetting, clitext.MembenchColOff, clitext.MembenchColOn,
		clitext.MembenchColDeltaSuccess, clitext.MembenchColDeltaTurns, clitext.MembenchColDeltaTokens,
		clitext.MembenchColMemoryTokens, clitext.MembenchColFalseRecall,
	}
	names := make([]string, 0, len(headers))
	for _, header := range headers {
		names = append(names, translator.Text(header, nil))
	}
	var text strings.Builder
	text.WriteString("| " + strings.Join(names, " | ") + " |\n")
	text.WriteString("|" + strings.Repeat("---|", len(headers)) + "\n")
	for i := range rows {
		text.WriteString("| " + strings.Join(rowCells(translator, lang, rows[i]), " | ") + " |\n")
	}
	return text.String()
}

func firstColumn(section clitext.Key) clitext.Key {
	if section == clitext.MembenchSectionLongMemEval {
		return clitext.MembenchColCapability
	}
	return clitext.MembenchColTask
}

func rowCells(translator *clitext.Translator, lang i18n.Language, item row) []string {
	return []string{
		translator.Text(item.Group, nil),
		item.Setting,
		percentCell(lang, item.Off),
		percentCell(lang, item.On),
		signed(lang, item.DeltaSuccess*percent, 0) + "%",
		signed(lang, item.DeltaTurns, turnDecimals),
		tokensCell(translator, lang, item.DeltaTokensPerSuccess),
		signed(lang, item.MemoryTokensPerTurn, memoryDecimals),
		fmt.Sprintf("%d / %d", item.Off.FalseRecalls, item.On.FalseRecalls),
	}
}

func percentCell(lang i18n.Language, counts cell) string {
	return fmt.Sprintf("%s%% (%d)", clitext.FormatNumber(lang, counts.successRate()*percent, 0), counts.Runs)
}

func tokensCell(translator *clitext.Translator, lang i18n.Language, delta *float64) string {
	if delta == nil {
		return translator.Text(clitext.MembenchNoMeasure, nil)
	}
	return signed(lang, *delta, 0)
}

func signed(lang i18n.Language, value float64, decimals int) string {
	text := clitext.FormatNumber(lang, value, decimals)
	if value > 0 {
		return signedPrefix + text
	}
	return text
}
