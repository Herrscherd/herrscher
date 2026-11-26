package host

import (
	"context"
	"fmt"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/clitext"
)

var errGatewayHasNoChannels = clitext.SentinelError("this gateway manages no channels")

type postingAdmin struct {
	gateway contracts.Gateway
	kind    string
}

func (p postingAdmin) Kind(context.Context, string) (string, error) {
	return "", fmt.Errorf("%s: %w", p.kind, errGatewayHasNoChannels)
}

func (p postingAdmin) CreateUnder(context.Context, string, string) (string, error) {
	return "", fmt.Errorf("%s: %w", p.kind, errGatewayHasNoChannels)
}

func (p postingAdmin) ForumPost(context.Context, string, string, string) (string, error) {
	return "", fmt.Errorf("%s: %w", p.kind, errGatewayHasNoChannels)
}

func (p postingAdmin) Archive(context.Context, string) error {
	return fmt.Errorf("%s: %w", p.kind, errGatewayHasNoChannels)
}

func (p postingAdmin) Send(ctx context.Context, channelID, content string) error {
	_, err := contracts.Degrade(p.gateway).Post(ctx, contracts.Conversation{Gateway: p.kind, ID: channelID}, content)
	return err
}

func (postingAdmin) ChannelRef(channelID string) string { return channelID }

func gatewayAdmins(gws []Deps) map[string]contracts.ChannelAdmin {
	admins := make(map[string]contracts.ChannelAdmin)
	for _, dep := range gws {
		if dep.Gateway == nil {
			continue
		}
		kind := dep.Gateway.Manifest().Kind
		if dep.Admin != nil {
			admins[kind] = dep.Admin
			continue
		}
		admins[kind] = postingAdmin{gateway: dep.Gateway, kind: kind}
	}
	return admins
}
