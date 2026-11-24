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
	statusFailed   = "failed"
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
	Section               msgKey   `json:"section"`
	Group                 msgKey   `json:"group"`
	Setting               string   `json:"setting"`
	Off                   cell     `json:"off"`
	On                    cell     `json:"on"`
	DeltaSuccess          float64  `json:"deltaSuccess"`
	DeltaTurns            float64  `json:"deltaTurns"`
	DeltaTokensPerSuccess *float64 `json:"deltaTokensPerSuccess"`
	MemoryTokensPerTurn   float64  `json:"memoryTokensPerTurn"`
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
	Failure     string  `json:"failure,omitempty"`
	Model       string  `json:"model"`
	LimitTokens int64   `json:"limitTokens"`
	LimitUSD    float64 `json:"limitUsd"`
	SpentTokens int64   `json:"spentTokens"`
	SpentUSD    float64 `json:"spentUsd"`
	NotRun      int     `json:"notRun"`
	Rows        []row   `json:"rows"`
}

type rowKey struct {
	section msgKey
	group   msgKey
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

func writeReport(out io.Writer, rep report, cfg config, msgs catalogue) error {
	if cfg.format == formatJSON {
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", jsonIndent)
		return encoder.Encode(rep)
	}
	return renderMarkdown(out, rep, msgs.For(cfg.lang))
}

func renderMarkdown(out io.Writer, rep report, translator *translator) error {
	var text strings.Builder
	text.WriteString("# " + translator.Text(msgReportTitle, nil) + "\n\n")
	text.WriteString(statusLine(translator, rep) + "\n")
	for _, section := range []msgKey{msgSectionPairs, msgSectionLongMemEval} {
		rows := rowsOf(rep.Rows, section)
		if len(rows) == 0 {
			continue
		}
		text.WriteString("\n## " + translator.Text(section, nil) + "\n\n")
		text.WriteString(table(translator, section, rows))
	}
	if len(rep.Rows) == 0 {
		text.WriteString("\n" + translator.Text(msgNoMeasure, nil) + "\n")
	}
	if err := translator.Err(); err != nil {
		return err
	}
	_, err := io.WriteString(out, text.String())
	return err
}

func statusLine(translator *translator, rep report) string {
	switch rep.Status {
	case statusFailed:
		return translator.Text(msgStatusFailed, params{paramDetail: rep.Failure}) + " " + notRunText(translator, rep)
	case statusPartial:
		stopped := translator.Text(msgBudgetStopped, params{
			paramSpent: amountText(translator, rep.SpentTokens, rep.SpentUSD),
			paramLimit: amountText(translator, rep.LimitTokens, rep.LimitUSD),
		})
		return translator.Text(msgStatusPartial, nil) + " " + stopped + " " + notRunText(translator, rep)
	}
	return translator.Text(msgStatusComplete, nil)
}

func notRunText(translator *translator, rep report) string {
	return translator.Text(msgNotRun, params{paramCount: clitext.FormatNumber(translator.lang, float64(rep.NotRun), 0)})
}

func amountText(translator *translator, tokens int64, usd float64) string {
	return translator.Text(msgAmount, params{
		paramTokens: clitext.FormatNumber(translator.lang, float64(tokens), 0),
		paramMoney:  clitext.FormatNumber(translator.lang, usd, moneyDecimals),
	})
}

func rowsOf(rows []row, section msgKey) []row {
	var matched []row
	for index := range rows {
		if rows[index].Section == section {
			matched = append(matched, rows[index])
		}
	}
	return matched
}

type column struct {
	header msgKey
	value  func(item row) string
}

func columnsOf(translator *translator, section msgKey) []column {
	lang := translator.lang
	columns := []column{
		{firstColumn(section), func(item row) string { return translator.Text(item.Group, nil) }},
		{msgColSetting, func(item row) string { return item.Setting }},
		{msgColOff, func(item row) string { return percentCell(lang, item.Off) }},
		{msgColOn, func(item row) string { return percentCell(lang, item.On) }},
		{msgColDeltaSuccess, func(item row) string { return signed(lang, item.DeltaSuccess*percent, 0) + "%" }},
		{msgColDeltaTurns, func(item row) string { return signed(lang, item.DeltaTurns, turnDecimals) }},
		{msgColDeltaVolume, func(item row) string { return tokensCell(translator, item.DeltaTokensPerSuccess) }},
		{msgColMemoryVolume, func(item row) string { return signed(lang, item.MemoryTokensPerTurn, memoryDecimals) }},
	}
	if section == msgSectionLongMemEval {
		return columns
	}
	return append(columns, column{msgColFalseRecall, func(item row) string {
		return fmt.Sprintf("%d / %d", item.Off.FalseRecalls, item.On.FalseRecalls)
	}})
}

func table(translator *translator, section msgKey, rows []row) string {
	columns := columnsOf(translator, section)
	headers := make([]string, 0, len(columns))
	for _, column := range columns {
		headers = append(headers, translator.Text(column.header, nil))
	}
	var text strings.Builder
	text.WriteString("| " + strings.Join(headers, " | ") + " |\n")
	text.WriteString("|" + strings.Repeat("---|", len(columns)) + "\n")
	for index := range rows {
		cells := make([]string, 0, len(columns))
		for _, column := range columns {
			cells = append(cells, column.value(rows[index]))
		}
		text.WriteString("| " + strings.Join(cells, " | ") + " |\n")
	}
	return text.String()
}

func firstColumn(section msgKey) msgKey {
	if section == msgSectionLongMemEval {
		return msgColCapability
	}
	return msgColTask
}

func percentCell(lang i18n.Language, counts cell) string {
	return fmt.Sprintf("%s%% (%d)", clitext.FormatNumber(lang, counts.successRate()*percent, 0), counts.Runs)
}

func tokensCell(translator *translator, delta *float64) string {
	if delta == nil {
		return translator.Text(msgNoMeasure, nil)
	}
	return signed(translator.lang, *delta, 0)
}

func signed(lang i18n.Language, value float64, decimals int) string {
	text := clitext.FormatNumber(lang, value, decimals)
	if value > 0 {
		return signedPrefix + text
	}
	return text
}
