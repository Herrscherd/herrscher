package usage

import (
	"bytes"
	"encoding/json"
	"strconv"
	"time"
)

const (
	claudeAssistantMark = `"type":"assistant"`
	claudeUsageMark     = `"usage"`
)

const (
	fnvOffset = 14695981039346656037
	fnvPrime  = 1099511628211
)

const (
	claudeFastSpeed     = "fast"
	claudeAdvisorType   = "advisor_message"
	claudeAdvisorSuffix = ":advisor:"
)

type claudeLine struct {
	Type      string        `json:"type"`
	Timestamp time.Time     `json:"timestamp"`
	SessionID string        `json:"sessionId"`
	RequestID string        `json:"requestId"`
	Message   claudeMessage `json:"message"`
}

type claudeMessage struct {
	ID    string       `json:"id"`
	Model string       `json:"model"`
	Usage *claudeUsage `json:"usage"`
}

type claudeUsage struct {
	claudeTokens

	Speed         string            `json:"speed"`
	CacheCreation claudeCacheTiers  `json:"cache_creation"`
	Iterations    []claudeIteration `json:"iterations"`
}

type claudeTokens struct {
	Input       int64 `json:"input_tokens"`
	CacheRead   int64 `json:"cache_read_input_tokens"`
	CacheCreate int64 `json:"cache_creation_input_tokens"`
	Output      int64 `json:"output_tokens"`
}

type claudeCacheTiers struct {
	Hour int64 `json:"ephemeral_1h_input_tokens"`
}

type claudeIteration struct {
	claudeTokens

	Type          string           `json:"type"`
	Model         string           `json:"model"`
	CacheCreation claudeCacheTiers `json:"cache_creation"`
}

type claudeReader struct {
	seen map[uint64]struct{}
}

func (r claudeReader) read(line []byte, emit func(tokenEvent)) {
	if !bytes.Contains(line, []byte(claudeAssistantMark)) || !bytes.Contains(line, []byte(claudeUsageMark)) {
		return
	}
	var parsed claudeLine
	if json.Unmarshal(line, &parsed) != nil || parsed.Type != "assistant" || parsed.Message.Usage == nil {
		return
	}
	model := parsed.Message.Model
	if !priceableName(model) || parsed.Timestamp.IsZero() {
		return
	}
	usage := parsed.Message.Usage
	dedupe := parsed.Message.ID + ":" + parsed.RequestID
	keyed := parsed.Message.ID != "" || parsed.RequestID != ""
	fast := usage.Speed == claudeFastSpeed
	if !keyed || r.first(dedupe) {
		emit(claudeEvent(parsed, model, fast, usage.claudeTokens, usage.CacheCreation))
	}
	r.advisors(parsed, usage, dedupe, keyed, emit)
}

func (r claudeReader) advisors(parsed claudeLine, usage *claudeUsage, dedupe string, keyed bool, emit func(tokenEvent)) {
	fast := usage.Speed == claudeFastSpeed
	for index, iteration := range usage.Iterations {
		if iteration.Type != claudeAdvisorType || !priceableName(iteration.Model) {
			continue
		}
		if keyed && !r.first(dedupe+claudeAdvisorSuffix+strconv.Itoa(index)) {
			continue
		}
		emit(claudeEvent(parsed, iteration.Model, fast, iteration.claudeTokens, iteration.CacheCreation))
	}
}

func claudeEvent(line claudeLine, model string, fast bool, tokens claudeTokens, tiers claudeCacheTiers) tokenEvent {
	return tokenEvent{
		at:              line.Timestamp,
		session:         line.SessionID,
		model:           model,
		fast:            fast,
		in:              tokens.Input,
		out:             tokens.Output,
		cacheRead:       tokens.CacheRead,
		cacheCreate:     tokens.CacheCreate,
		cacheCreateHour: tiers.Hour,
	}
}

func (r claudeReader) first(key string) bool {
	sum := fnv64(key)
	if _, ok := r.seen[sum]; ok {
		return false
	}
	r.seen[sum] = struct{}{}
	return true
}

func priceableName(model string) bool {
	return model != "" && model != "<synthetic>"
}

func fnv64(text string) uint64 {
	sum := uint64(fnvOffset)
	for index := range len(text) {
		sum ^= uint64(text[index])
		sum *= fnvPrime
	}
	return sum
}
