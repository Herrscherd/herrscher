package host

import (
	"context"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

type approvalRelayingBackend interface {
	RespondWithApprovals(ctx context.Context, prompt contracts.Prompt, onEvent func(contracts.BackendEvent)) (string, error)
}

type remoteBackend struct {
	contracts.Backend
}

func (b remoteBackend) Respond(ctx context.Context, prompt contracts.Prompt, onEvent func(contracts.BackendEvent)) (string, error) {
	prompt.Outbox = ""
	if _, asked := contracts.ApprovalAskerFrom(ctx); asked {
		if relay, ok := b.Backend.(approvalRelayingBackend); ok {
			return relay.RespondWithApprovals(ctx, prompt, onEvent)
		}
	}
	return b.Backend.Respond(ctx, prompt, onEvent)
}
