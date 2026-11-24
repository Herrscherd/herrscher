package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"strings"

	"github.com/Herrscherd/herrscher/core/clitext"
)

const (
	abstentionSuffix = "_abs"
	rememberPrefix   = "Remember this: "
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

func loadLongMemEval(path string, samples int, seed uint64, maxTurns int) ([]task, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, messageError{key: clitext.MembenchErrDatasetRead, params: clitext.Params{clitext.ParamPath: path, clitext.ParamErr: err.Error()}}
	}
	items, eligible, readErr := sampleItems(file, samples, seed, maxTurns)
	closeErr := file.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, messageError{key: clitext.MembenchErrDatasetRead, params: clitext.Params{clitext.ParamPath: path, clitext.ParamErr: closeErr.Error()}}
	}
	if eligible == 0 {
		return nil, datasetShape("no item has evidence sessions, a known question type and an answer")
	}
	tasks := make([]task, 0, len(items))
	for i := range items {
		tasks = append(tasks, itemTask(items[i]))
	}
	return tasks, nil
}

func sampleItems(source io.Reader, samples int, seed uint64, maxTurns int) ([]lmeItem, int, error) {
	decoder := json.NewDecoder(source)
	if err := expectDelim(decoder, '['); err != nil {
		return nil, 0, err
	}
	rng := rand.New(rand.NewPCG(seed, 0))
	var reservoir []lmeItem
	eligible := 0
	for decoder.More() {
		var item lmeItem
		if err := decoder.Decode(&item); err != nil {
			return nil, 0, messageError{key: clitext.MembenchErrDatasetShape, params: clitext.Params{clitext.ParamDetail: err.Error()}}
		}
		if !eligibleItem(item, maxTurns) {
			continue
		}
		eligible++
		if len(reservoir) < samples {
			reservoir = append(reservoir, item)
		} else if slot := rng.IntN(eligible); slot < samples {
			reservoir[slot] = item
		}
	}
	if err := expectDelim(decoder, ']'); err != nil {
		return nil, 0, err
	}
	return reservoir, eligible, nil
}

func expectDelim(decoder *json.Decoder, want json.Delim) error {
	token, err := decoder.Token()
	if err != nil {
		return messageError{key: clitext.MembenchErrDatasetShape, params: clitext.Params{clitext.ParamDetail: err.Error()}}
	}
	if delim, ok := token.(json.Delim); !ok || delim != want {
		return datasetShape(fmt.Sprintf("expected %q, got %v", want, token))
	}
	return nil
}

func datasetShape(detail string) error {
	return messageError{key: clitext.MembenchErrDatasetShape, params: clitext.Params{clitext.ParamDetail: detail}}
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

func capabilityOf(item lmeItem) (clitext.Key, bool) {
	if isAbstention(item) {
		return clitext.MembenchCapAbstention, true
	}
	switch item.QuestionType {
	case "single-session-user", "single-session-assistant", "single-session-preference":
		return clitext.MembenchCapExtraction, true
	case "multi-session":
		return clitext.MembenchCapMultisession, true
	case "temporal-reasoning":
		return clitext.MembenchCapTemporal, true
	case "knowledge-update":
		return clitext.MembenchCapUpdate, true
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
		section: clitext.MembenchSectionLongMemEval,
		group:   capability,
		id:      item.QuestionID,
		steps:   steps,
		ask:     step{agent: agentAlpha, prompt: item.Question},
	}
	if isAbstention(item) {
		built.abstain = true
		return built
	}
	built.want = []string{strings.ToLower(strings.TrimSpace(answerText(item)))}
	return built
}

func transcript(turns []lmeTurn) string {
	lines := make([]string, 0, len(turns))
	for _, turn := range turns {
		lines = append(lines, turn.Role+": "+turn.Content)
	}
	return strings.Join(lines, "\n")
}
