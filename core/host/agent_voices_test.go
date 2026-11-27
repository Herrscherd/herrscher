package host

import (
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/agent"
)

type bindingGateway struct {
	kindGateway
	bound contracts.SessionControl
}

func (g *bindingGateway) BindSessionControl(control contracts.SessionControl) {
	g.bound = control
}

func TestGatewayReceivesTheAgentDirectoryThroughDegrade(t *testing.T) {
	store := agent.NewStore(t.TempDir())
	if _, err := store.Create(agent.CreateSpec{Name: "rin"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	h := newTestHub(t)
	h.voices = agent.NewDirectory(store, []string{"DISCORD_BOT_TOKEN_RIN=secret-value"}, nil)
	gateway := &bindingGateway{kindGateway: kindGateway{kind: "discord"}}
	bindSessionControl([]Deps{{Gateway: contracts.Degrade(gateway)}}, h)

	directory, ok := gateway.bound.(contracts.AgentDirectory)
	if !ok {
		t.Fatalf("the control a gateway receives is %T, which is not an AgentDirectory", gateway.bound)
	}
	voices := directory.AgentVoices("discord")
	if len(voices) != 1 || voices[0].Agent != "rin" || voices[0].Token.Secret() != "secret-value" {
		t.Fatalf("voices = %+v; want rin with its token", voices)
	}
}
