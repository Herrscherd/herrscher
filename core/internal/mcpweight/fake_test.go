package mcpweight

import (
	"bufio"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/agent"
)

const (
	fakeModeEnv    = "HERRSCHER_MCPWEIGHT_FAKE_MODE"
	fakePIDFileEnv = "HERRSCHER_MCPWEIGHT_PIDFILE"
	fakeModeGrand  = "grandchild"
	bulkyPageSize  = 3 << 20
	pageOneTools   = `[{"name":"alpha","description":"first tool","inputSchema":{"type":"object"}},{"name":"beta","description":"second tool","inputSchema":{"type":"object","properties":{"q":{"type":"string"}}}}]`
	pageTwoTools   = `[{"name":"gamma","description":"third tool","inputSchema":{"type":"object"}}]`
	fakeToolCount  = 3
)

func fakeToolDefinitions() []string {
	return []string{
		`{"name":"alpha","description":"first tool","inputSchema":{"type":"object"}}`,
		`{"name":"beta","description":"second tool","inputSchema":{"type":"object","properties":{"q":{"type":"string"}}}}`,
		`{"name":"gamma","description":"third tool","inputSchema":{"type":"object"}}`,
	}
}

func expectedBytes() int {
	return len(strings.Join(fakeToolDefinitions(), ""))
}

func TestFakeServer(_ *testing.T) {
	mode := os.Getenv(fakeModeEnv)
	switch mode {
	case "":
		return
	case fakeModeGrand:
		time.Sleep(time.Hour)
	}
	serveFake(mode)
	os.Exit(0)
}

func fakeServer(mode string) agent.MCPServer {
	return fakeServerWithEnv(mode, nil)
}

func fakeServerWithEnv(mode string, extra map[string]string) agent.MCPServer {
	env := map[string]string{fakeModeEnv: mode}
	maps.Copy(env, extra)
	return agent.MCPServer{
		Name:    "fake-" + mode,
		Command: os.Args[0],
		Args:    []string{"-test.run=^TestFakeServer$"},
		Env:     env,
	}
}

func serveFake(mode string) {
	if err := spawnGrandchildFor(mode); err != nil {
		os.Exit(5)
	}
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
		switch mode {
		case "exit":
			os.Exit(3)
		case "stderr-exit":
			fmt.Fprint(os.Stderr, "missing token")
			os.Exit(3)
		case "silent", "forking-silent":
			continue
		case "garbage":
			reply("not json at all")
			continue
		case "server-request":
			reply(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"sampling/createMessage","params":{}}`, *message.ID))
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
	case mode == "bulky":
		return fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{"tools":[{"name":"heavy","description":%q}],"nextCursor":"more"}}`, id, strings.Repeat("x", bulkyPageSize))
	case mode == "no-tools-array":
		return fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{}}`, id)
	case cursor == "":
		return fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{"tools":%s,"nextCursor":"page-2"}}`, id, pageOneTools)
	default:
		return fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{"tools":%s}}`, id, pageTwoTools)
	}
}
