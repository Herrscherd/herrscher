package main

import (
	"context"
	"errors"
	"log/slog"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	discordgateway "github.com/Herrscherd/herrscher-discord-gateway"
)

var errNoNoticeSink = errors.New("gateway cannot deliver notices")

func deliverNotice(ctx context.Context, gateway contracts.Gateway, conv contracts.Conversation, lang i18n.Language, notice contracts.Notice) error {
	sink, ok := contracts.Degrade(gateway).(contracts.NoticeSink)
	if !ok {
		return errNoNoticeSink
	}
	_, err := sink.Notify(ctx, conv, lang, notice)
	if errors.Is(err, discordgateway.ErrFallbackPosted) {
		slog.Warn("notice posted as fallback text, not resent", "err", err)
		return nil
	}
	return err
}
