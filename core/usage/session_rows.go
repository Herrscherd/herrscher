package usage

import (
	"cmp"
	"maps"
	"slices"
)

// Injection is what the host wrote into the prompt of a session's turns: how many turns
// carry the sizes of the blocks it injected, and their estimated tokens, at about four
// bytes per token and not a count from the vendor.
type Injection struct {
	Turns  int   `json:"turns"`
	Tokens int64 `json:"tokens"`
}

// Average is the estimated tokens injected per turn, false when no turn carries sizes.
func (i Injection) Average() (float64, bool) {
	if i.Turns == 0 {
		return 0, false
	}
	return float64(i.Tokens) / float64(i.Turns), true
}

// SessionRow is what one session spent during the period: its cost and tokens, the share
// of its input read from the prompt cache as a percentage, and what the host injected.
type SessionRow struct {
	Session    string    `json:"session"`
	Cost       float64   `json:"cost"`
	Tokens     Tokens    `json:"tokens"`
	CacheRatio float64   `json:"cacheRatio"`
	Injection  Injection `json:"injection"`
}

// SessionRows gives each named session of the records its row, by name, most costly first.
// The records are the herrscher ledger of the period: a compaction and an unnamed record
// carry no row.
func SessionRows(records []Record) []SessionRow {
	rows := make(map[string]SessionRow)
	for index := range records {
		record := &records[index]
		if record.Compaction != nil || record.Session == "" {
			continue
		}
		rows[record.Session] = addToRow(rows[record.Session], record)
	}
	return sortedSessionRows(rows)
}

func addToRow(row SessionRow, record *Record) SessionRow {
	row.Session = record.Session
	row.Cost += record.Cost
	row.Tokens = row.Tokens.plus(recordTokens(*record))
	if record.InjectedMeasured {
		row.Injection.Turns++
		row.Injection.Tokens += record.InjectedTokens
	}
	return row
}

func sortedSessionRows(rows map[string]SessionRow) []SessionRow {
	out := make([]SessionRow, 0, len(rows))
	for _, name := range slices.Sorted(maps.Keys(rows)) {
		row := rows[name]
		row.CacheRatio = cacheRatio(row.Tokens)
		out = append(out, row)
	}
	slices.SortStableFunc(out, func(left, right SessionRow) int {
		return cmp.Compare(right.Cost, left.Cost)
	})
	return out
}

func recordTokens(record Record) Tokens {
	return Tokens{In: record.In, Out: record.Out, CacheRead: record.CacheRead, CacheCreate: record.CacheCreate}
}

func (t Tokens) plus(other Tokens) Tokens {
	return Tokens{In: t.In + other.In, Out: t.Out + other.Out, CacheRead: t.CacheRead + other.CacheRead, CacheCreate: t.CacheCreate + other.CacheCreate}
}
