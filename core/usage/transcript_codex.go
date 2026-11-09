package usage

import (
	"bytes"
	"encoding/json"
	"slices"
	"time"
)

func codexMarks() []string {
	return []string{`"session_meta"`, `"turn_context"`, `"token_count"`, `"thread_settings_applied"`}
}

func codexFastTiers() []string {
	return []string{"fast", "priority", "ultrafast"}
}

const codexForkCopyGap = time.Second

type codexLine struct {
	Type      string       `json:"type"`
	Timestamp time.Time    `json:"timestamp"`
	Payload   codexPayload `json:"payload"`
}

type codexPayload struct {
	Type       string          `json:"type"`
	ID         string          `json:"id"`
	SessionID  string          `json:"session_id"`
	ForkedFrom string          `json:"forked_from_id"`
	Source     json.RawMessage `json:"source"`
	Model      string          `json:"model"`
	Info       *codexInfo      `json:"info"`
	Settings   *codexSettings  `json:"thread_settings"`
}

type codexSource struct {
	Subagent struct {
		Spawn struct {
			Parent string `json:"parent_thread_id"`
		} `json:"thread_spawn"`
	} `json:"subagent"`
}

type codexSettings struct {
	Tier string `json:"service_tier"`
}

type codexInfo struct {
	Last  *codexTokens `json:"last_token_usage"`
	Total *codexTokens `json:"total_token_usage"`
}

type codexTokens struct {
	Input      int64 `json:"input_tokens"`
	Cached     int64 `json:"cached_input_tokens"`
	CacheWrite int64 `json:"cache_write_input_tokens"`
	Output     int64 `json:"output_tokens"`
	Reasoning  int64 `json:"reasoning_output_tokens"`
	Total      int64 `json:"total_tokens"`
}

type codexReader struct {
	session    string
	model      string
	fast       bool
	sawMeta    bool
	forking    bool
	forkAnchor time.Time
	lastTotal  *codexTokens
	lastSeen   *codexTokens
}

func (r *codexReader) read(line []byte, emit func(tokenEvent)) {
	if !slices.ContainsFunc(codexMarks(), func(mark string) bool { return bytes.Contains(line, []byte(mark)) }) {
		return
	}
	var parsed codexLine
	if json.Unmarshal(line, &parsed) != nil {
		return
	}
	payload := parsed.Payload
	switch {
	case parsed.Type == "session_meta":
		r.meta(parsed)
	case parsed.Type == "turn_context":
		r.model = payload.Model
	case payload.Type == "thread_settings_applied" && payload.Settings != nil:
		r.fast = slices.Contains(codexFastTiers(), payload.Settings.Tier)
	case payload.Type == "token_count" && payload.Info != nil:
		r.count(parsed.Timestamp, *payload.Info, emit)
	}
}

func (r *codexReader) meta(line codexLine) {
	if r.sawMeta {
		return
	}
	r.sawMeta = true
	r.session = line.Payload.ID
	if r.session == "" {
		r.session = line.Payload.SessionID
	}
	if line.Payload.ForkedFrom != "" || spawnedBy(line.Payload.Source) != "" {
		r.forking, r.forkAnchor = true, line.Timestamp
	}
}

func spawnedBy(raw json.RawMessage) string {
	var source codexSource
	if json.Unmarshal(raw, &source) != nil {
		return ""
	}
	return source.Subagent.Spawn.Parent
}

func (r *codexReader) count(when time.Time, info codexInfo, emit func(tokenEvent)) {
	if r.model == "" || when.IsZero() {
		return
	}
	if r.forkCopy(when) {
		if info.Total != nil {
			r.lastTotal = info.Total
		}
		return
	}
	previous := r.lastTotal
	if info.Total != nil {
		r.lastTotal = info.Total
	}
	last := info.Last
	switch {
	case last == nil || last.Input+last.Output == 0:
		r.lastSeen = nil
		return
	case info.Total != nil && previous != nil && *info.Total == *previous:
		return
	case info.Total == nil && r.lastSeen != nil && *last == *r.lastSeen:
		return
	}
	r.lastSeen = last
	emit(tokenEvent{
		at:          when,
		session:     r.session,
		model:       r.model,
		fast:        r.fast,
		in:          max(last.Input-last.Cached-last.CacheWrite, 0),
		out:         last.Output,
		cacheRead:   last.Cached,
		cacheCreate: last.CacheWrite,
	})
}

func (r *codexReader) forkCopy(when time.Time) bool {
	if !r.forking {
		return false
	}
	if when.Sub(r.forkAnchor) < codexForkCopyGap {
		r.forkAnchor = when
		return true
	}
	r.forking, r.lastSeen = false, nil
	return false
}
