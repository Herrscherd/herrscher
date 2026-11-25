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
	FalseRecalls *int  `json:"falseRecalls"`
	Turns        int   `json:"turns"`
	Tokens       int64 `json:"tokens"`
	Input        int64 `json:"input"`
	falseRecalls int
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
		c.falseRecalls++
	}
}

func (c *cell) publishFalseRecalls(measured bool) {
	if !measured {
		c.FalseRecalls = nil
		return
	}
	count := c.falseRecalls
	c.FalseRecalls = &count
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
	Paired                int      `json:"paired"`
	Partial               bool     `json:"partial"`
	DeltaSuccess          *float64 `json:"deltaSuccess"`
	DeltaTurns            *float64 `json:"deltaTurns"`
	DeltaTokensPerSuccess *float64 `json:"deltaTokensPerSuccess"`
	MemoryTokensPerTurn   *float64 `json:"memoryTokensPerTurn"`
}

type pairing struct {
	off []outcome
	on  []outcome
}

func (r *row) add(got outcome) {
	if got.on {
		r.On.add(got)
		return
	}
	r.Off.add(got)
}

func (r *row) derive(tasks map[string]*pairing) {
	measured := r.Section != msgSectionLongMemEval
	r.Off.publishFalseRecalls(measured)
	r.On.publishFalseRecalls(measured)
	var pairedOff, pairedOn cell
	for _, sides := range tasks {
		if len(sides.off) != len(sides.on) {
			r.Partial = true
		}
		for index := range min(len(sides.off), len(sides.on)) {
			pairedOff.add(sides.off[index])
			pairedOn.add(sides.on[index])
			r.Paired++
		}
	}
	if r.Paired == 0 {
		return
	}
	r.deriveDeltas(pairedOff, pairedOn)
}

func (r *row) deriveDeltas(offCell, onCell cell) {
	success := onCell.successRate() - offCell.successRate()
	turns := onCell.averageTurns() - offCell.averageTurns()
	memory := onCell.inputPerTurn() - offCell.inputPerTurn()
	r.DeltaSuccess = &success
	r.DeltaTurns = &turns
	r.MemoryTokensPerTurn = &memory
	onTokens, onMeasured := onCell.tokensPerSuccess()
	offTokens, offMeasured := offCell.tokensPerSuccess()
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
	SkippedLong int     `json:"skippedTooLong"`
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
	tasks := make(map[rowKey]map[string]*pairing)
	for i := range executed.outcomes {
		got := executed.outcomes[i]
		key := rowKey{section: got.section, group: got.group, setting: got.setting}
		position, seen := index[key]
		if !seen {
			rep.Rows = append(rep.Rows, row{Section: got.section, Group: got.group, Setting: got.setting})
			position = len(rep.Rows) - 1
			index[key] = position
			tasks[key] = make(map[string]*pairing)
		}
		rep.Rows[position].add(got)
		sides := tasks[key][got.task]
		if sides == nil {
			sides = &pairing{}
			tasks[key][got.task] = sides
		}
		if got.on {
			sides.on = append(sides.on, got)
		} else {
			sides.off = append(sides.off, got)
		}
	}
	for key, position := range index {
		rep.Rows[position].derive(tasks[key])
	}
	return rep
}

func writeReport(out io.Writer, rep report, cfg config, msgs catalogue) error {
	if cfg.format == formatJSON {
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", jsonIndent)
		if err := encoder.Encode(rep); err != nil {
			return reportError(err)
		}
		return nil
	}
	if err := renderMarkdown(out, rep, msgs.For(cfg.lang)); err != nil {
		return reportError(err)
	}
	return nil
}

func renderMarkdown(out io.Writer, rep report, translator *translator) error {
	var text strings.Builder
	text.WriteString("# " + translator.Text(msgReportTitle, nil) + "\n\n")
	text.WriteString(statusLine(translator, rep) + "\n")
	if rep.SkippedLong > 0 {
		text.WriteString(translator.Text(msgSkippedLong, params{paramCount: clitext.FormatNumber(translator.lang, float64(rep.SkippedLong), 0)}) + "\n")
	}
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
		{firstColumn(section), func(item row) string { return groupText(translator, item) }},
		{msgColSetting, func(item row) string { return item.Setting }},
		{msgColOff, func(item row) string { return percentCell(lang, item.Off) }},
		{msgColOn, func(item row) string { return percentCell(lang, item.On) }},
		{msgColDeltaSuccess, func(item row) string { return optionalSigned(translator, item.DeltaSuccess, percent, 0, "%") }},
		{msgColDeltaTurns, func(item row) string { return optionalSigned(translator, item.DeltaTurns, 1, turnDecimals, "") }},
		{msgColDeltaVolume, func(item row) string { return tokensCell(translator, item.DeltaTokensPerSuccess) }},
		{msgColMemoryVolume, func(item row) string {
			return optionalSigned(translator, item.MemoryTokensPerTurn, 1, memoryDecimals, "")
		}},
	}
	if section == msgSectionLongMemEval {
		return columns
	}
	return append(columns, column{msgColFalseRecall, func(item row) string {
		return fmt.Sprintf("%d / %d", item.Off.falseRecalls, item.On.falseRecalls)
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

func groupText(translator *translator, item row) string {
	name := translator.Text(item.Group, nil)
	if item.Partial {
		return name + " (" + translator.Text(msgRowPartial, nil) + ")"
	}
	return name
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
	return optionalSigned(translator, delta, 1, 0, "")
}

func optionalSigned(translator *translator, value *float64, scale float64, decimals int, suffix string) string {
	if value == nil {
		return translator.Text(msgNoMeasure, nil)
	}
	return signed(translator.lang, *value*scale, decimals) + suffix
}

func signed(lang i18n.Language, value float64, decimals int) string {
	text := clitext.FormatNumber(lang, value, decimals)
	if value > 0 {
		return signedPrefix + text
	}
	return text
}
