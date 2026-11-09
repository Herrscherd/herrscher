package manager

import (
	"context"
	"log/slog"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/usage"
)

// SetAccountFallback wires the account balancer of new sessions. Until it is set,
// a new session keeps the account it was planned on.
func (h *Handler) SetAccountFallback(fallback accountFallback) {
	h.fallback = fallback
}

func (h *Handler) balanceAccount(ctx context.Context, runtime runtimeChoice, hostName, planned string, explicit bool) (string, *contracts.Notice) {
	if h.fallback == nil || explicit || hostName != "" || h.remoteBackend {
		return planned, nil
	}
	vendor, err := h.launchVendor(runtime)
	if err != nil {
		slog.Warn("account fallback skipped, vendor not resolved", "err", err)
		return planned, nil
	}
	choice := h.fallback.Balance(ctx, vendor, planned)
	switch choice.Outcome {
	case usage.ChoiceSwitched:
		if !h.fallbackTargetUsable(hostName, runtime, choice.To) {
			return planned, nil
		}
		notice := contracts.Notice{Code: contracts.CodeAccountFallback, Args: []contracts.Arg{contracts.Text("From", choice.From), contracts.Text("To", choice.To)}}
		return choice.To, &notice
	case usage.ChoiceNoQuota:
		notice := contracts.Notice{Code: contracts.CodeAccountNoQuota, Args: []contracts.Arg{contracts.Text("Vendor", vendor), contracts.Text("Account", choice.From)}}
		return planned, &notice
	case usage.ChoiceKept:
	}
	return planned, nil
}

func (h *Handler) fallbackTargetUsable(hostName string, runtime runtimeChoice, account string) bool {
	if err := h.checkAccountUsable(hostName, account); err != nil {
		slog.Warn("account fallback target refused, planned account kept", "err", err)
		return false
	}
	if err := h.checkAccount(runtime, account); err != nil {
		slog.Warn("account fallback target refused, planned account kept", "err", err)
		return false
	}
	return true
}

func (h *Handler) announceAccount(ctx context.Context, req createRequest, channelID string) {
	if req.notice == nil {
		return
	}
	h.fallback.Notify(ctx, req.home, channelID, h.noticeLanguage(req.lang), *req.notice)
}
