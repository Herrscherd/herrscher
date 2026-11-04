package host

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"sync"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

const (
	overviewWaiting  = "waiting"
	overviewWorking  = "working"
	overviewArchived = "archived"
	overviewIdle     = "idle"
	overviewTextMax  = 240
)

type SessionOverview struct {
	Name     string   `json:"name"`
	Vendor   string   `json:"vendor"`
	Gateways []string `json:"gateways"`
	State    string   `json:"state"`
	Cost     float64  `json:"cost"`
	LastTs   string   `json:"last_ts"`
	LastText string   `json:"last_text"`
}

func classifySession(waiting, working, archived bool) string {
	switch {
	case waiting:
		return overviewWaiting
	case working:
		return overviewWorking
	case archived:
		return overviewArchived
	default:
		return overviewIdle
	}
}

type transcriptTally struct {
	offset   int64
	cost     float64
	lastTs   string
	lastText string
}

type tallyCache struct {
	mu    sync.Mutex
	files map[string]*transcriptTally
}

func newTallyCache() *tallyCache {
	return &tallyCache{files: map[string]*transcriptTally{}}
}

func (c *tallyCache) tally(path string) transcriptTally {
	c.mu.Lock()
	defer c.mu.Unlock()
	cur := c.files[path]
	if cur == nil {
		cur = &transcriptTally{}
		c.files[path] = cur
	}
	advanceTally(path, cur)
	return *cur
}

func advanceTally(path string, cur *transcriptTally) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return
	}
	if info.Size() < cur.offset {
		*cur = transcriptTally{}
	}
	if _, err := file.Seek(cur.offset, io.SeekStart); err != nil {
		return
	}
	reader := bufio.NewReader(file)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return
		}
		cur.offset += int64(len(line))
		applyTranscriptLine(cur, line)
	}
}

func applyTranscriptLine(cur *transcriptTally, line []byte) {
	var entry state.TranscriptEntry
	if json.Unmarshal(line, &entry) != nil {
		return
	}
	cur.cost += entry.Cost
	if entry.Ts != "" {
		cur.lastTs = entry.Ts
	}
	if entry.Text != "" {
		cur.lastText = clipOverviewText(entry.Text)
	}
}

func clipOverviewText(text string) string {
	runes := []rune(text)
	if len(runes) > overviewTextMax {
		return string(runes[:overviewTextMax])
	}
	return text
}

type overviewSource struct {
	st        *state.State
	partDir   string
	sessions  *sessionTable
	approvals *approvalTable
	tallies   *tallyCache
}

func (src overviewSource) overviews() []SessionOverview {
	waiting := map[string]bool{}
	for _, pending := range pendingApprovals(src.approvals) {
		waiting[pending.Session] = true
	}
	sessions := src.st.SnapshotSessions()
	out := make([]SessionOverview, 0, len(sessions))
	for _, live := range sessions {
		tally := src.tallies.tally(state.TranscriptPath(src.partDir, live.Name))
		driver := src.sessions.lookup(live.Name)
		working := driver != nil && driver.busy.Load()
		out = append(out, SessionOverview{
			Name:     live.Name,
			Vendor:   live.Vendor,
			Gateways: live.BoundGateways(),
			State:    classifySession(waiting[live.Name], working, live.Archived),
			Cost:     tally.cost,
			LastTs:   tally.lastTs,
			LastText: tally.lastText,
		})
	}
	return out
}
