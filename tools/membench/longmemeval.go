package main

import (
	"cmp"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
)

const (
	abstentionSuffix = "_abs"
	rememberPrefix   = "Remember this: "
	maxDatasetBytes  = 512 << 20
	maxDatasetItems  = 100000
	fnvOffset        = 14695981039346656037
	fnvPrime         = 1099511628211
)

type lmeTurn struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type lmeItem struct {
	QuestionID   string      `json:"question_id"`
	QuestionType string      `json:"question_type"`
	Question     string      `json:"question"`
	Answer       any         `json:"answer"`
	SessionIDs   []string    `json:"haystack_session_ids"`
	Sessions     [][]lmeTurn `json:"haystack_sessions"`
	Evidence     []string    `json:"answer_session_ids"`
}

type sampled struct {
	key  uint64
	item lmeItem
}

func loadLongMemEval(path string, samples int, seed uint64, maxTurns int) ([]task, int, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, datasetRead(path, err)
	}
	limited := &io.LimitedReader{R: file, N: maxDatasetBytes + 1}
	items, eligible, skipped, readErr := sampleItems(limited, samples, seed, maxTurns)
	closeErr := file.Close()
	if limited.N <= 0 {
		return nil, 0, datasetLarge(strconv.Itoa(maxDatasetBytes) + " bytes")
	}
	if readErr != nil {
		return nil, 0, readErr
	}
	if closeErr != nil {
		return nil, 0, datasetRead(path, closeErr)
	}
	if eligible == 0 {
		return nil, 0, datasetEmpty()
	}
	tasks := make([]task, 0, len(items))
	for i := range items {
		tasks = append(tasks, itemTask(items[i]))
	}
	return tasks, skipped, nil
}

func sampleItems(source io.Reader, samples int, seed uint64, maxTurns int) ([]lmeItem, int, int, error) {
	decoder := json.NewDecoder(source)
	if err := expectDelim(decoder, '['); err != nil {
		return nil, 0, 0, err
	}
	var kept []sampled
	decoded, eligible, skipped := 0, 0, 0
	for decoder.More() {
		var item lmeItem
		if err := decoder.Decode(&item); err != nil {
			return nil, 0, 0, datasetMalformed(decoder.InputOffset())
		}
		decoded++
		if decoded > maxDatasetItems {
			return nil, 0, 0, datasetLarge(strconv.Itoa(maxDatasetItems) + " items")
		}
		if !eligibleItem(item, maxTurns) {
			continue
		}
		eligible++
		if oversizedItem(item) {
			skipped++
			continue
		}
		kept = append(kept, sampled{key: sampleKey(seed, item.QuestionID), item: item})
		if len(kept) >= 2*samples {
			kept = lowestKeys(kept, samples)
		}
	}
	if err := expectDelim(decoder, ']'); err != nil {
		return nil, 0, 0, err
	}
	kept = lowestKeys(kept, samples)
	items := make([]lmeItem, 0, len(kept))
	for i := range kept {
		items = append(items, kept[i].item)
	}
	return items, eligible, skipped, nil
}

func oversizedItem(item lmeItem) bool {
	if len(item.Question) > maxPromptBytes {
		return true
	}
	for _, index := range evidenceSessions(item) {
		if len(rememberPrefix)+len(transcript(item.Sessions[index])) > maxPromptBytes {
			return true
		}
	}
	return false
}

func sampleKey(seed uint64, itemID string) uint64 {
	hash := uint64(fnvOffset)
	for _, b := range binary.LittleEndian.AppendUint64(nil, seed) {
		hash = (hash ^ uint64(b)) * fnvPrime
	}
	for i := range len(itemID) {
		hash = (hash ^ uint64(itemID[i])) * fnvPrime
	}
	return hash
}

func lowestKeys(kept []sampled, limit int) []sampled {
	slices.SortFunc(kept, func(a, b sampled) int {
		return cmp.Or(cmp.Compare(a.key, b.key), strings.Compare(a.item.QuestionID, b.item.QuestionID))
	})
	if len(kept) > limit {
		kept = kept[:limit]
	}
	return kept
}

func expectDelim(decoder *json.Decoder, want json.Delim) error {
	token, err := decoder.Token()
	if err != nil {
		return datasetMalformed(decoder.InputOffset())
	}
	if delim, ok := token.(json.Delim); !ok || delim != want {
		return datasetDelim(want, decoder.InputOffset())
	}
	return nil
}

func datasetRead(path string, cause error) error {
	switch {
	case errors.Is(cause, os.ErrNotExist):
		return pathError(msgErrDatasetMissing, path)
	case errors.Is(cause, os.ErrPermission):
		return pathError(msgErrDatasetDenied, path)
	}
	return pathError(msgErrDatasetRead, path)
}

func pathError(key msgKey, path string) error {
	return messageError{key: key, params: params{paramPath: path}}
}

func datasetLarge(limit string) error {
	return messageError{key: msgErrDatasetLarge, params: params{paramLimit: limit}}
}

func datasetEmpty() error {
	return messageError{key: msgErrDatasetEmpty}
}

func datasetMalformed(offset int64) error {
	return messageError{key: msgErrDatasetJSON, params: params{paramOffset: offset}}
}

func datasetDelim(want json.Delim, offset int64) error {
	return messageError{key: msgErrDatasetDelim, params: params{paramWant: want.String(), paramOffset: offset}}
}

func eligibleItem(item lmeItem, maxTurns int) bool {
	if _, known := capabilityOf(item); !known {
		return false
	}
	if len(evidenceSessions(item)) == 0 || len(evidenceSessions(item))+1 > maxTurns {
		return false
	}
	return isAbstention(item) || strings.TrimSpace(answerText(item)) != ""
}

func capabilityOf(item lmeItem) (msgKey, bool) {
	if isAbstention(item) {
		return msgCapAbstention, true
	}
	switch item.QuestionType {
	case "single-session-user", "single-session-assistant", "single-session-preference":
		return msgCapExtraction, true
	case "multi-session":
		return msgCapMultisession, true
	case "temporal-reasoning":
		return msgCapTemporal, true
	case "knowledge-update":
		return msgCapUpdate, true
	}
	return "", false
}

func isAbstention(item lmeItem) bool {
	return strings.HasSuffix(item.QuestionID, abstentionSuffix)
}

func answerText(item lmeItem) string {
	return fmt.Sprint(item.Answer)
}

func evidenceSessions(item lmeItem) []int {
	evidence := make(map[string]bool, len(item.Evidence))
	for _, id := range item.Evidence {
		evidence[id] = true
	}
	var indexes []int
	for index, id := range item.SessionIDs {
		if evidence[id] && index < len(item.Sessions) {
			indexes = append(indexes, index)
		}
	}
	return indexes
}

func itemTask(item lmeItem) task {
	capability, _ := capabilityOf(item)
	var steps []step
	for _, index := range evidenceSessions(item) {
		steps = append(steps, step{agent: agentAlpha, prompt: rememberPrefix + transcript(item.Sessions[index])})
	}
	built := task{
		section: msgSectionLongMemEval,
		group:   capability,
		id:      item.QuestionID,
		steps:   steps,
		ask:     step{agent: agentAlpha, prompt: item.Question},
	}
	if isAbstention(item) {
		built.abstain = true
		return built
	}
	built.want = []string{answerText(item)}
	return built
}

func transcript(turns []lmeTurn) string {
	lines := make([]string, 0, len(turns))
	for _, turn := range turns {
		lines = append(lines, turn.Role+": "+turn.Content)
	}
	return strings.Join(lines, "\n")
}
