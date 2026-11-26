package mcpweight

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

const fakeModeEnv = "HERRSCHER_MCPWEIGHT_FAKE_MODE"

const (
	pageOneTools = `[{"name":"alpha","description":"first tool","inputSchema":{"type":"object"}},{"name":"beta","description":"second tool","inputSchema":{"type":"object","properties":{"q":{"type":"string"}}}}]`
	pageTwoTools = `[{"name":"gamma","description":"third tool","inputSchema":{"type":"object"}}]`
)

func fakeToolDefinitions() []string {
	return []string{
		`{"name":"alpha","description":"first tool","inputSchema":{"type":"object"}}`,
		`{"name":"beta","description":"second tool","inputSchema":{"type":"object","properties":{"q":{"type":"string"}}}}`,
		`{"name":"gamma","description":"third tool","inputSchema":{"type":"object"}}`,
	}
}

func TestFakeServer(_ *testing.T) {
	mode := os.Getenv(fakeModeEnv)
	if mode == "" {
		return
	}
	serveFake(mode)
	os.Exit(0)
}

func fakeServer(mode string) Server {
	return Server{
		Name:    "fake-" + mode,
		Command: os.Args[0],
		Args:    []string{"-test.run=^TestFakeServer$"},
		Env:     map[string]string{fakeModeEnv: mode},
	}
}

func serveFake(mode string) {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var message struct {
			ID     *int   `json:"id"`
			Method string `json:"method"`
			Params struct {
				Cursor string `json:"cursor"`
			} `json:"params"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
			return
		}
		if message.ID == nil {
			continue
		}
		if mode == "exit" {
			os.Exit(3)
		}
		if mode == "silent" {
			time.Sleep(time.Hour)
		}
		if mode == "garbage" {
			reply("not json at all")
			continue
		}
		reply(`{"jsonrpc":"2.0","method":"notifications/message","params":{"level":"info"}}`)
		reply(fakeAnswer(mode, *message.ID, message.Method, message.Params.Cursor))
	}
}

func reply(line string) {
	if _, err := os.Stdout.WriteString(line + "\n"); err != nil {
		os.Exit(4)
	}
}

func fakeAnswer(mode string, id int, method, cursor string) string {
	switch {
	case method == methodInitialize:
		return fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{"protocolVersion":%q,"capabilities":{"tools":{}},"serverInfo":{"name":"fake","version":"0"}}}`, id, protocolVersion)
	case mode == "rejecting":
		return fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"error":{"code":-32601,"message":"no such method"}}`, id)
	case mode == "endless":
		return fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{"tools":[],"nextCursor":"more"}}`, id)
	case mode == "no-tools-array":
		return fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{}}`, id)
	case cursor == "":
		return fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{"tools":%s,"nextCursor":"page-2"}}`, id, pageOneTools)
	default:
		return fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{"tools":%s}}`, id, pageTwoTools)
	}
}

func expectedBytes() int {
	return len(strings.Join(fakeToolDefinitions(), ""))
}
