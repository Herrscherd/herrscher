package host

import (
	"context"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/internal/approval"
)

type frenchMenuGateway struct {
	eventGateway

	menus []string
}

func (g *frenchMenuGateway) Manifest() contracts.Manifest {
	return contracts.Manifest{Kind: "rec", Capabilities: contracts.Capabilities{SelectMenus: true}}
}

func (g *frenchMenuGateway) Menu(_ context.Context, _ contracts.Conversation, _ contracts.MessageID, prompt string, _ []contracts.Choice) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.menus = append(g.menus, prompt)
	return nil
}

func TestApprovalMenuAndHelpFollowTheSessionLanguage(t *testing.T) {
	tests := []struct {
		name string
		lang i18n.Language
		want string
	}{
		{
			name: "french session gets the whole menu in french",
			lang: i18n.French,
			want: "autoriser Bash(ls) ? (répondez avec `approve allow|deny id1`, voir `approve allow --help`)",
		},
		{
			name: "english session keeps the english menu",
			lang: i18n.English,
			want: "allow Bash(ls)? (answer with `approve allow|deny id1`, see `approve allow --help`)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gw := &frenchMenuGateway{eventGateway: eventGateway{menuGatewayFake: &menuGatewayFake{}}}
			d := &sessionDriver{name: "s1", lang: tt.lang, gateways: []contracts.GatewaySet{{Gateway: gw, Reader: gw}}}
			d.askApproval(context.Background(), "id1", approval.Request{Tool: "Bash", Subject: "ls"}, time.Now().Add(time.Minute))
			gw.mu.Lock()
			defer gw.mu.Unlock()
			if len(gw.menus) != 1 || gw.menus[0] != tt.want {
				t.Fatalf("menu = %q, want [%q]", gw.menus, tt.want)
			}
			if !hasSubjectEvent(gw.emitted, "id1", "Bash(ls)") {
				t.Fatalf("no approval_subject event for the TUI: %+v", gw.emitted)
			}
		})
	}
}

func hasSubjectEvent(events []contracts.Event, id, subject string) bool {
	for index := range events {
		if events[index].T == approvalSubjectEventKind && events[index].Value == id && events[index].Text == subject {
			return true
		}
	}
	return false
}
