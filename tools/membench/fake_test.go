package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	fakeEnv        = "MEMBENCH_FAKE"
	fakeFactsFile  = "facts.txt"
	fakeNoAnswer   = "I don't know."
	fakeNoted      = "Noted."
	fakeCorrection = "Correction: "
	fakeBaseIn     = 100
	fakePerFact    = 10
	fakeOut        = 10
	fakeCost       = 0.001
	fakeDirMode    = 0o700
	fakeFileMode   = 0o600
	fakeMinWord    = 5
	fakeExitFailed = 1
	fakeExitUsage  = 2
)

type fakeRecord struct {
	Time  time.Time `json:"time"`
	Model string    `json:"model"`
	In    int64     `json:"in"`
	Out   int64     `json:"out"`
	Cost  float64   `json:"cost"`
}

func fakeHerrscher(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	if len(args) != 2 || args[0] != "--print" {
		return fakeFail(stderr, errors.New("fake herrscher: expected --print and one prompt"), fakeExitUsage)
	}
	answer, err := fakeTurn(getenv("HERRSCHER_STATE_DIR"), getenv("MEMORY_RAW_ARCHIVE") == "true", getenv("HERRSCHER_DEFAULT_MODEL"), args[1])
	if err != nil {
		return fakeFail(stderr, err, fakeExitFailed)
	}
	if _, writeErr := fmt.Fprint(stdout, answer); writeErr != nil {
		return fakeFail(stderr, writeErr, fakeExitFailed)
	}
	return 0
}

func fakeFail(stderr io.Writer, cause error, code int) int {
	if _, writeErr := fmt.Fprintln(stderr, cause); writeErr != nil {
		return fakeExitFailed
	}
	return code
}

func fakeTurn(state string, memory bool, model, prompt string) (string, error) {
	facts, err := readFakeFacts(state)
	if err != nil {
		return "", err
	}
	answer := fakeNoAnswer
	switch {
	case isTeaching(prompt):
		answer = fakeNoted
		if memory {
			err = appendFakeFact(state, facts, teachingFact(prompt))
		}
	case memory:
		if fact, found := latestFakeMatch(facts, prompt); found {
			answer = fact
		}
	}
	if err != nil {
		return "", err
	}
	input := int64(fakeBaseIn)
	if memory {
		input += int64(fakePerFact * len(facts))
	}
	record := fakeRecord{Time: time.Now().UTC(), Model: model, In: input, Out: fakeOut, Cost: fakeCost}
	if err := appendFakeUsage(state, record); err != nil {
		return "", err
	}
	return answer, nil
}

func isTeaching(prompt string) bool {
	return strings.HasPrefix(prompt, rememberPrefix) || strings.HasPrefix(prompt, fakeCorrection)
}

func teachingFact(prompt string) string {
	_, fact, _ := strings.Cut(prompt, ": ")
	return fact
}

func appendFakeFact(state string, facts []string, fact string) error {
	facts = append(facts, fact)
	return os.WriteFile(filepath.Join(state, fakeFactsFile), []byte(strings.Join(facts, "\n")), fakeFileMode)
}

func readFakeFacts(state string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(state, fakeFactsFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n"), nil
}

func latestFakeMatch(facts []string, question string) (string, bool) {
	words := wordsOf(question)
	for i := len(facts) - 1; i >= 0; i-- {
		lower := strings.ToLower(facts[i])
		for _, word := range words {
			if strings.Contains(lower, word) {
				return facts[i], true
			}
		}
	}
	return "", false
}

func wordsOf(text string) []string {
	var words []string
	for _, field := range strings.FieldsFunc(text, notWordRune) {
		lower := strings.ToLower(field)
		if utf8.RuneCountInString(lower) >= fakeMinWord {
			words = append(words, lower)
		}
	}
	return words
}

func notWordRune(r rune) bool {
	return !unicode.IsLetter(r) && !unicode.IsDigit(r)
}

func appendFakeUsage(state string, record fakeRecord) error {
	dir := filepath.Join(state, usageSubdir)
	if err := os.MkdirAll(dir, fakeDirMode); err != nil {
		return err
	}
	line, err := json.Marshal(record)
	if err != nil {
		return err
	}
	name := filepath.Join(dir, record.Time.Format(time.DateOnly)+".jsonl")
	file, err := os.OpenFile(name, os.O_APPEND|os.O_CREATE|os.O_WRONLY, fakeFileMode)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(append(line, '\n'))
	return errors.Join(writeErr, file.Close())
}
