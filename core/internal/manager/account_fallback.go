package manager

import (
	"context"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/usage"
)

// SetAccountFallback wires the account balancer of new sessions. Until it is set,
// a new session keeps the account it was planned on.
func (h *Handler) SetAccountFallback(fallback accountFallback) {
	h.fallback = fallback
}

func (h *Handler) balanceAccount(ctx context.Context, runtime runtimeChoice, planned string) (string, *contracts.Notice, error) {
	if h.fallback == nil {
		return planned, nil, nil
	}
	vendor, err := h.launchVendor(runtime)
	if err != nil {
		return "", nil, err
	}
	choice := h.fallback.Balance(ctx, vendor, planned)
	switch choice.Outcome {
	case usage.ChoiceSwitched:
		notice := contracts.Notice{Code: contracts.CodeAccountFallback, Args: []contracts.Arg{contracts.Text("From", choice.From), contracts.Text("To", choice.To)}}
		return choice.To, &notice, nil
	case usage.ChoiceNoQuota:
		notice := contracts.Notice{Code: contracts.CodeAccountNoQuota, Args: []contracts.Arg{contracts.Text("Vendor", vendor), contracts.Text("Account", choice.From)}}
		return planned, &notice, nil
	case usage.ChoiceKept:
	}
	return planned, nil, nil
}

func (h *Handler) announceAccount(ctx context.Context, req createRequest, channelID string) {
	if req.notice == nil {
		return
	}
	h.fallback.Notify(ctx, req.home, channelID, h.noticeLanguage(req.lang), *req.notice)
}
