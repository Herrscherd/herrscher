package vendortest

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type event struct {
	Type    string `json:"type"`
	IsError bool   `json:"is_error"`
	Result  string `json:"result"`
	Message struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
			ID   string `json:"id"`
			Name string `json:"name"`
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
	var out, errOut bytes.Buffer
	code := Claude(streamArgs(), Streams{In: strings.NewReader(input), Out: &out, Err: &errOut}, files)
	var events []event
	for line := range strings.Lines(out.String()) {
		var decoded event
		if err := json.Unmarshal([]byte(line), &decoded); err != nil {
			tb.Fatalf("not an event: %q", line)
		}
		events = append(events, decoded)
	}
	return code, events
}

func summary(events []event) []string {
	var got []string
	for _, e := range events {
		switch e.Type {
		case "assistant":
			for _, block := range e.Message.Content {
				got = append(got, block.Type+":"+block.Text+block.Name+block.ID)
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
		{"tool", "tool Bash {\"command\":\"ls\"}\ntext done", []string{"go"}, 0, []string{"tool_use:Bashtoolu_1", "text:done", "result:done"}},
		{"error", "error quota exhausted", []string{"go"}, 0, []string{"failed:quota exhausted"}},
		{"echo past the end", "", []string{"first line\nlast line"}, 0, []string{"text:echo: last line", "result:echo: last line"}},
		{"crash", "text partial\ncrash", []string{"go"}, exitFailure, []string{"text:partial"}},
		{"hang", "hang", []string{"go", "again"}, 0, nil},
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
