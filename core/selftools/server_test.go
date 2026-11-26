package selftools_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/Herrscherd/herrscher/core/selftools"
)

const specsJSON = `[
 {"path":["session","list"],"help":"List the sessions."},
 {"path":["schedule","add"],"help":"Add a schedule.","params":[
  {"Name":"name","Help":"Schedule name","Required":true,"ValueRequired":true},
  {"Name":"task","Help":"Task","Required":true,"ValueRequired":true},
  {"Name":"every","Help":"Period","Required":false,"ValueRequired":true}]},
 {"path":["approve","allow"],"help":"Allow a call.","params":[
  {"Name":"session","Help":"Session","Required":false,"ValueRequired":false}]}
]`

var errRefused = errors.New("herrscher: `schedule add` is not something a session may run")

type recordingDaemon struct {
	mu     sync.Mutex
	calls  [][]string
	answer map[string]string
	fail   map[string]error
}

func (d *recordingDaemon) Dispatch(_ context.Context, argv []string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, slices.Clone(argv))
	verb := strings.Join(argv, " ")
	if verb == "commands --json" {
		return specsJSON, nil
	}
	for prefix, err := range d.fail {
		if strings.HasPrefix(verb, prefix) {
			return "", err
		}
	}
	return d.answer[verb], nil
}

func (d *recordingDaemon) verbCalls() [][]string {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out [][]string
	for _, call := range d.calls {
		if call[0] != "commands" {
			out = append(out, call)
		}
	}
	return out
}

type rpcReply struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code int `json:"code"`
	} `json:"error"`
}

func serve(t *testing.T, daemon selftools.Daemon, exposure selftools.Exposure, lines ...string) []rpcReply {
	t.Helper()
	var out bytes.Buffer
	server := selftools.NewServer(daemon, "test", exposure)
	if err := server.Serve(t.Context(), strings.NewReader(strings.Join(lines, "\n")+"\n"), &out); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	var replies []rpcReply
	decoder := json.NewDecoder(&out)
	for decoder.More() {
		var reply rpcReply
		if err := decoder.Decode(&reply); err != nil {
			t.Fatalf("decode reply: %v", err)
		}
		replies = append(replies, reply)
	}
	return replies
}

func toolNames(t *testing.T, reply rpcReply) []string {
	t.Helper()
	var result struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(reply.Result, &result); err != nil {
		t.Fatalf("decode tools: %v", err)
	}
	names := make([]string, 0, len(result.Tools))
	for _, tool := range result.Tools {
		names = append(names, tool.Name)
	}
	return names
}

type callOutcome struct {
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
	IsError bool `json:"isError"`
}

func outcomeOf(t *testing.T, reply rpcReply) callOutcome {
	t.Helper()
	if reply.Error != nil {
		t.Fatalf("call answered a protocol error %d, want a tool result", reply.Error.Code)
	}
	var outcome callOutcome
	if err := json.Unmarshal(reply.Result, &outcome); err != nil {
		t.Fatalf("decode call result: %v", err)
	}
	return outcome
}

func TestToolsListExposesTheGenericToolsAndTheVerbsOfTheExposure(t *testing.T) {
	cases := []struct {
		exposure selftools.Exposure
		want     []string
	}{
		{selftools.ExposureCore, []string{"herrscher_commands", "herrscher_run", "session_list"}},
		{selftools.ExposureAll, []string{"herrscher_commands", "herrscher_run", "session_list", "schedule_add", "approve_allow"}},
	}
	for _, tc := range cases {
		replies := serve(t, &recordingDaemon{}, tc.exposure, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
		got := toolNames(t, replies[0])
		if !slices.Equal(got, tc.want) {
			t.Errorf("exposure %d: tools = %v, want %v", tc.exposure, got, tc.want)
		}
	}
}

func TestRunSendsTheVerbWithItsNamedParametersAsInlineFlags(t *testing.T) {
	daemon := &recordingDaemon{answer: map[string]string{"schedule add --name=digest --task=--read the PRs --every=30m --json": "added"}}
	replies := serve(t, daemon, selftools.ExposureCore,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"herrscher_run","arguments":{"command":"schedule add","args":{"name":"digest","task":"--read the PRs","every":"30m"},"json":true}}}`)
	outcome := outcomeOf(t, replies[0])
	if outcome.IsError || outcome.Content[0].Text != "added" {
		t.Fatalf("outcome = %+v, want the daemon's answer", outcome)
	}
	want := []string{"schedule", "add", "--name=digest", "--task=--read the PRs", "--every=30m", "--json"}
	if calls := daemon.verbCalls(); len(calls) != 1 || !slices.Equal(calls[0], want) {
		t.Fatalf("daemon saw %q, want %q", calls, want)
	}
}

func TestATypedVerbToolCarriesBooleanFlagsAndPositionals(t *testing.T) {
	daemon := &recordingDaemon{}
	serve(t, daemon, selftools.ExposureAll,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"approve_allow","arguments":{"session":true,"rest":["a1b2"]}}}`)
	want := []string{"approve", "allow", "--session=true", "a1b2"}
	if calls := daemon.verbCalls(); len(calls) != 1 || !slices.Equal(calls[0], want) {
		t.Fatalf("daemon saw %q, want %q", calls, want)
	}
}

func TestRunRefusesWhatWouldReachTheDaemonAsSomethingElse(t *testing.T) {
	cases := map[string]string{
		"a flag smuggled in rest": `{"command":"approve allow","rest":["--always"]}`,
		"an undeclared parameter": `{"command":"session list","args":{"all":true}}`,
		"a verb the daemon lacks": `{"command":"role grant"}`,
		"no command at all":       `{"args":{}}`,
		"args that are not a map": `{"command":"session list","args":["x"]}`,
		"an object as a value":    `{"command":"schedule add","args":{"name":{"x":1},"task":"t"}}`,
	}
	for name, arguments := range cases {
		daemon := &recordingDaemon{}
		replies := serve(t, daemon, selftools.ExposureCore,
			`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"herrscher_run","arguments":`+arguments+`}}`)
		if outcome := outcomeOf(t, replies[0]); !outcome.IsError {
			t.Errorf("%s: outcome = %+v, want a refusal", name, outcome)
		}
		if calls := daemon.verbCalls(); len(calls) != 0 {
			t.Errorf("%s: the daemon ran %q", name, calls)
		}
	}
}

func TestADaemonRefusalReachesTheModelAsAToolError(t *testing.T) {
	daemon := &recordingDaemon{fail: map[string]error{"schedule add": errRefused}}
	replies := serve(t, daemon, selftools.ExposureCore,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"herrscher_run","arguments":{"command":"schedule add","args":{"name":"n","task":"t"}}}}`)
	outcome := outcomeOf(t, replies[0])
	if !outcome.IsError || outcome.Content[0].Text != errRefused.Error() {
		t.Fatalf("outcome = %+v, want the daemon's refusal as a tool error", outcome)
	}
}

func TestNotificationsGetNoAnswerAndUnknownMethodsAnError(t *testing.T) {
	replies := serve(t, &recordingDaemon{}, selftools.ExposureCore,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":7,"method":"resources/list"}`,
		`{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"nope"}}`)
	if len(replies) != 2 {
		t.Fatalf("replies = %+v, want two, the notification unanswered", replies)
	}
	codes := make(map[int]int)
	for _, reply := range replies {
		if reply.Error != nil {
			codes[reply.ID] = reply.Error.Code
		}
	}
	if codes[7] != -32601 || codes[8] != -32602 {
		t.Errorf("error codes by id = %v, want 7 method not found and 8 invalid params", codes)
	}
}

type blockingDaemon struct {
	recordingDaemon

	release chan struct{}
}

func (d *blockingDaemon) Dispatch(ctx context.Context, argv []string) (string, error) {
	if strings.Join(argv, " ") == "schedule add --name=n --task=t" {
		<-d.release
	}
	return d.recordingDaemon.Dispatch(ctx, argv)
}

func TestACallWaitingOnTheOperatorDoesNotHoldTheOthersBack(t *testing.T) {
	daemon := &blockingDaemon{release: make(chan struct{})}
	in, feed := io.Pipe()
	out, read := io.Pipe()
	server := selftools.NewServer(daemon, "test", selftools.ExposureCore)
	served := make(chan error, 1)
	go func() { served <- server.Serve(t.Context(), in, read) }()
	answers := json.NewDecoder(out)

	write := func(line string) {
		if _, err := io.WriteString(feed, line+"\n"); err != nil {
			t.Fatal(err)
		}
	}
	write(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"herrscher_run","arguments":{"command":"schedule add","args":{"name":"n","task":"t"}}}}`)
	write(`{"jsonrpc":"2.0","id":2,"method":"ping"}`)
	var first rpcReply
	if err := answers.Decode(&first); err != nil || first.ID != 2 {
		t.Fatalf("first answer = %+v, %v, want the ping while the call waits", first, err)
	}
	close(daemon.release)
	var second rpcReply
	if err := answers.Decode(&second); err != nil || second.ID != 1 {
		t.Fatalf("second answer = %+v, %v, want the released call", second, err)
	}
	if err := feed.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-served; err != nil {
		t.Fatalf("Serve: %v", err)
	}
}

func TestInitializeKeepsAKnownProtocolVersionAndOffersTheLatestOtherwise(t *testing.T) {
	cases := map[string]string{"2025-03-26": "2025-03-26", "1999-01-01": "2025-06-18"}
	for asked, want := range cases {
		replies := serve(t, &recordingDaemon{}, selftools.ExposureCore,
			`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"`+asked+`"}}`)
		var result struct {
			ProtocolVersion string `json:"protocolVersion"`
			ServerInfo      struct {
				Name string `json:"name"`
			} `json:"serverInfo"`
		}
		if err := json.Unmarshal(replies[0].Result, &result); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if result.ProtocolVersion != want || result.ServerInfo.Name != selftools.ServerName {
			t.Errorf("asked %s: got %+v, want version %s", asked, result, want)
		}
	}
}
