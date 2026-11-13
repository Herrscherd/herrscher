package vendortest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
)

type event struct {
	Type    string `json:"type"`
	IsError bool   `json:"is_error"`
	Result  string `json:"result"`
	Message struct {
		Content []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
	} `json:"message"`
}

func streamArgs() []string {
	return []string{"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose"}
}

func promptLine(tb testing.TB, text string) string {
	tb.Helper()
	line, err := json.Marshal(map[string]any{"type": "user", "message": map[string]string{"role": "user", "content": text}})
	if err != nil {
		tb.Fatal(err)
	}
	return string(line) + "\n"
}

func scriptFiles(tb testing.TB, script string) Files {
	tb.Helper()
	dir := tb.TempDir()
	files := Files{Script: filepath.Join(dir, "claude.script"), Log: filepath.Join(dir, "claude.log"), Turns: filepath.Join(dir, "claude.turns")}
	if err := os.WriteFile(files.Script, []byte(script), 0o600); err != nil {
		tb.Fatal(err)
	}
	return files
}

func run(tb testing.TB, files Files, input string) (int, []event) {
	tb.Helper()
	code, events, err := replayed(files, input)
	if err != nil {
		tb.Fatal(err)
	}
	return code, events
}

var errNotEvent = errors.New("the fake wrote a line that is not an event")

func replayed(files Files, input string) (int, []event, error) {
	var out, errOut bytes.Buffer
	code := Claude(streamArgs(), Streams{In: strings.NewReader(input), Out: &out, Err: &errOut}, files)
	var events []event
	for line := range strings.Lines(out.String()) {
		var decoded event
		if err := json.Unmarshal([]byte(line), &decoded); err != nil {
			return code, nil, fmt.Errorf("%w: %q", errNotEvent, line)
		}
		events = append(events, decoded)
	}
	return code, events, nil
}

func summary(events []event) []string {
	var got []string
	for _, e := range events {
		switch e.Type {
		case "assistant":
			for _, block := range e.Message.Content {
				got = append(got, block.Type+":"+block.Text+block.Name+block.ID+string(block.Input))
			}
		case "result":
			if e.IsError {
				got = append(got, "failed:"+e.Result)
			} else {
				got = append(got, "result:"+e.Result)
			}
		}
	}
	return got
}

func TestClaudeReplaysEachStep(t *testing.T) {
	for _, tc := range []struct {
		name   string
		script string
		input  []string
		code   int
		want   []string
	}{
		{"text", "text hi\ntext there", []string{"go"}, 0, []string{"text:hi", "text:there", "result:hi\n\nthere"}},
		{"tool", "tool Bash {\"command\":\"ls\"}\ntext done", []string{"go"}, 0, []string{"tool_use:Bashtoolu_1{\"command\":\"ls\"}", "text:done", "result:done"}},
		{"error", "error quota exhausted", []string{"go"}, 0, []string{"failed:quota exhausted"}},
		{"echo past the end", "", []string{"first line\nlast line"}, 0, []string{"text:echo: last line", "result:echo: last line"}},
		{"crash", "text partial\ncrash", []string{"go"}, exitFailure, []string{"text:partial"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var input strings.Builder
			for _, text := range tc.input {
				input.WriteString(promptLine(t, text))
			}
			code, events := run(t, scriptFiles(t, tc.script), input.String())
			if got := summary(events); code != tc.code || strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Fatalf("got exit %d %q, want exit %d %q", code, got, tc.code, tc.want)
			}
		})
	}
}

func TestClaudeCountsTurnsAcrossProcesses(t *testing.T) {
	files := scriptFiles(t, "text one\n---\ntext two\n")
	var got []string
	for range 2 {
		_, events := run(t, files, promptLine(t, "go"))
		got = append(got, summary(events)...)
	}
	if want := "text:one|result:one|text:two|result:two"; strings.Join(got, "|") != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	logged, err := os.ReadFile(files.Log)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(logged), "prompt \"go\"") != 2 {
		t.Fatalf("log: %q", logged)
	}
}

func TestClaudeHangsUntilStdinCloses(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		files := scriptFiles(t, "hang")
		stdin, feed := io.Pipe()
		var out bytes.Buffer
		done := make(chan int, 1)
		go func() {
			done <- Claude(streamArgs(), Streams{In: stdin, Out: &out, Err: io.Discard}, files)
		}()
		prompts := promptLine(t, "go") + promptLine(t, "again")
		written := make(chan error, 1)
		go func() {
			_, err := io.WriteString(feed, prompts)
			written <- err
		}()
		synctest.Wait()
		select {
		case code := <-done:
			t.Fatalf("returned %d while stdin was still open", code)
		default:
		}
		if err := <-written; err != nil {
			t.Fatal(err)
		}
		if err := feed.Close(); err != nil {
			t.Fatal(err)
		}
		if code := <-done; code != 0 || out.Len() != 0 {
			t.Fatalf("got exit %d and %q, want a silent exit 0", code, out.String())
		}
	})
}

func TestClaudeGivesConcurrentProcessesDistinctTurns(t *testing.T) {
	const fakes = 8
	var script strings.Builder
	for turn := range fakes {
		fmt.Fprintf(&script, "text t%d\n---\n", turn)
	}
	files := scriptFiles(t, script.String())
	prompt := promptLine(t, "go")
	replies := make(chan string, fakes)
	failures := make(chan error, fakes)
	var group sync.WaitGroup
	for range fakes {
		group.Go(func() {
			_, events, err := replayed(files, prompt)
			failures <- err
			replies <- strings.Join(summary(events), "|")
		})
	}
	group.Wait()
	close(replies)
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	seen := make(map[string]bool, fakes)
	for reply := range replies {
		seen[reply] = true
	}
	if len(seen) != fakes {
		t.Fatalf("got %d distinct turns out of %d: %v", len(seen), fakes, seen)
	}
}

func TestLockTurnsNamesAStaleLock(t *testing.T) {
	turns := filepath.Join(t.TempDir(), "claude.turns")
	if err := os.WriteFile(turns+".lock", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := lockTurnsWithin(turns, 0)
	locked, isLocked := errors.AsType[*TurnsLockedError](err)
	if !isLocked || locked.Lock != turns+".lock" {
		t.Fatalf("got %v, want the stale lock named", err)
	}
}
