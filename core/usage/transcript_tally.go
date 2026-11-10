package usage

import "time"

type tokenEvent struct {
	at              time.Time
	session         string
	model           string
	fast            bool
	in              int64
	out             int64
	cacheRead       int64
	cacheCreate     int64
	cacheCreateHour int64
}

func (e tokenEvent) empty() bool {
	return e.in+e.out+e.cacheRead+e.cacheCreate == 0
}

func (e tokenEvent) contextBand() int64 {
	context := e.in + e.cacheRead + e.cacheCreate
	var band int64
	for _, threshold := range contextThresholds() {
		if context > threshold {
			band = threshold
		}
	}
	return band
}

type tallyKey struct {
	hour     int64
	session  string
	model    string
	fast     bool
	longTier int64
}

type tally struct {
	vendor  string
	account string
	records []Record
	index   map[tallyKey]int
}

func newTally(vendor, account string, records []Record) *tally {
	index := make(map[tallyKey]int, len(records))
	for at := range records {
		index[keyOf(records[at])] = at
	}
	return &tally{vendor: vendor, account: account, records: records, index: index}
}

func keyOf(record Record) tallyKey {
	return tallyKey{hour: record.Time.Unix(), session: record.Session, model: record.Model, fast: record.Fast, longTier: record.LongTier}
}

func (t *tally) add(event tokenEvent) {
	if event.empty() {
		return
	}
	hour := event.at.UTC().Truncate(time.Hour)
	longTier := event.contextBand()
	key := tallyKey{hour: hour.Unix(), session: event.session, model: event.model, fast: event.fast, longTier: longTier}
	slot, ok := t.index[key]
	if !ok {
		slot = len(t.records)
		t.index[key] = slot
		t.records = append(t.records, Record{Time: hour, Session: event.session, Vendor: t.vendor, Account: t.account, Model: event.model, Fast: event.fast, LongTier: longTier})
	}
	record := &t.records[slot]
	record.In += event.in
	record.Out += event.out
	record.CacheRead += event.cacheRead
	record.CacheCreate += event.cacheCreate
	record.CacheCreateHour += event.cacheCreateHour
}
