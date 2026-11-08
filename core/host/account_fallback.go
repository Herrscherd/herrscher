package host

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/usage"
)

const balanceTimeout = 5 * time.Second

type accountFallback struct {
	source   *usageSource
	gateways []Deps
	timeout  time.Duration
}

func newAccountFallback(source *usageSource, gateways []Deps) *accountFallback {
	return &accountFallback{source: source, gateways: gateways, timeout: balanceTimeout}
}

func (f *accountFallback) Balance(ctx context.Context, vendor, planned string) usage.Choice {
	ctx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()
	return usage.ChooseAccount(planned, f.readVendor(ctx, vendor))
}

func (f *accountFallback) readVendor(ctx context.Context, vendor string) []usage.AccountLimits {
	targets := slices.DeleteFunc(f.source.targets(usage.AccountKey{}), func(target usage.Target) bool {
		return target.Vendor != vendor
	})
	return f.source.limits.Read(ctx, defaultFirst(targets))
}

func defaultFirst(targets []usage.Target) []usage.Target {
	if !slices.ContainsFunc(targets, func(target usage.Target) bool { return target.Default }) {
		for index := range targets {
			if targets[index].Account == "" {
				targets[index].Default = true
			}
		}
	}
	slices.SortStableFunc(targets, func(left, right usage.Target) int {
		switch {
		case left.Default == right.Default:
			return 0
		case left.Default:
			return -1
		default:
			return 1
		}
	})
	return targets
}

func (f *accountFallback) Notify(ctx context.Context, home state.HomeRef, channelID string, lang i18n.Language, notice contracts.Notice) {
	gateway, ok := gatewayForHome(f.gateways, home)
	if !ok {
		slog.Warn("account notice dropped, no gateway for the home", "code", notice.Code)
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), noticeTimeout)
	defer cancel()
	conv := contracts.Conversation{Gateway: gateway.Gateway.Manifest().Kind, ID: channelID}
	if sink, ok := gateway.Gateway.(contracts.NoticeSink); ok {
		_, err := sink.Notify(ctx, conv, lang, notice)
		if err == nil || errors.Is(err, contracts.ErrNoticeFallbackPosted) {
			return
		}
		slog.Warn("account notice not rendered by the gateway", "code", notice.Code, "err", err)
	}
	text, err := contracts.NewRenderer().Render(lang, notice)
	if err != nil {
		slog.Warn("account notice not rendered", "code", notice.Code, "err", err)
		return
	}
	if err := gateway.Admin.Send(ctx, channelID, text); err != nil {
		slog.Warn("account notice not sent", "code", notice.Code, "err", err)
	}
}
